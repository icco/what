package main

import (
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"appengine"
	"appengine/datastore"
	"appengine/file"

	"github.com/golang/oauth2/google"
	"google.golang.org/cloud"
	"google.golang.org/cloud/storage"
)

type Note struct {
	Author  string
	Content string
	Date    time.Time
}

func init() {
	http.HandleFunc("/", root)
	http.HandleFunc("/_ah/mail/", incomingMail)
}

// noteKey returns the key used for all note entries.
func noteKey(c appengine.Context) *datastore.Key {
	return datastore.NewKey(c, "Note", "notes", 0, nil)
}

func root(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	q := datastore.NewQuery("Note").Ancestor(noteKey(c)).Order("-Date").Limit(10)
	notes := make([]Note, 0, 10)
	if _, err := q.GetAll(c, &notes); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := noteTemplate.Execute(w, notes); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// TODO(icco): Move to seperate file.
var noteTemplate = template.Must(template.New("book").Parse(`
<html>
  <head>
    <title>Nat Notes</title>
  </head>
  <body>
    {{range .}}
      {{with .Author}}
        <p><b>{{.}}</b> wrote:</p>
      {{else}}
        <p>An anonymous person wrote:</p>
      {{end}}
      <pre>{{.Content}}</pre>
    {{end}}
  </body>
</html>
`))

func incomingMail(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	defer r.Body.Close()
	parsed, err := mail.ReadMessage(r.Body)
	if err != nil {
		c.Errorf("Error parsing mail: %v", err)
		return
	}
	body, err := parseBody(parsed.Body, parsed.Header.Get("Content-Type"))
	if err != nil {
		c.Errorf("Failed reading body: %v", err)
		return
	}
	c.Infof("Parsed mail headers: %+v", parsed.Header)
	for k, v := range body {
		c.Infof("Parse mail body part %d (type: %v): %+v", k, v.ContentType, string(v.Data))
		if err := v.Store(c); err != nil {
			c.Errorf("Failed storing message: %v", err)
		}
	}
}

// A Message is a datatype the represents a part of the email body. Often a
// body will have a text/plain and a text/html message inside it, plus a
// message for each attachment. ContentType and Data will always have something
// in them, the rest of the fields might be nil.
type Message struct {
	ContentType             string
	Data                    []byte
	ContentId               string
	ContentTransferEncoding string
	ContentDisposition      string
}

// Writes Message to appropriate storage place.
func (m *Message) Store(c appengine.Context) error {
	if strings.HasPrefix(m.ContentType, "text/") || strings.HasPrefix(m.ContentType, "multipart/alternative") {
		return m._datastoreSave(c)
	} else {
		c.Infof("Starting blobstore: {ID: %v, Encoding: %v, Type: %v, Disposition: %v}", m.ContentId, m.ContentTransferEncoding, m.ContentType, m.ContentDisposition)
		return m._blobstoreSave(c)
	}
}

// Takes an io.Reader and turns it into to a Message struct array.
func parseBody(body io.Reader, contentType string) ([]*Message, error) {
	messages := make([]*Message, 0)
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return nil, err
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				return messages, nil
			}
			if err != nil {
				return nil, err
			}

			slurp, err := ioutil.ReadAll(p)
			if err != nil {
				return nil, err
			}
			messages = append(messages, &Message{
				ContentType:             p.Header.Get("Content-Type"),
				Data:                    slurp,
				ContentId:               contentIdString(p.Header.Get("Content-ID")),
				ContentTransferEncoding: p.Header.Get("Content-Transfer-Encoding"),
				ContentDisposition:      p.Header.Get("Content-Disposition"),
			})
		}
	} else {
		return nil, errors.New(fmt.Sprintf("Unknown mediatype: %+v", mediaType))
	}
}

func (m *Message) _datastoreSave(c appengine.Context) error {
	mediaType, params, err := mime.ParseMediaType(m.ContentType)
	if err != nil {
		return nil, err
	}

	content = ""
	if strings.HasPrefix(mediaType, "multipart/") {
		messages := map[string]string{}
		mr := multipart.NewReader(m.Data, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			} else if err != nil {
				return nil, err
			}

			slurp, err := ioutil.ReadAll(p)
			if err != nil {
				return nil, err
			}

			messages[p.Header.Get("content-type")] = string(slurp)
		}

		if messages["text/html; charset=UTF-8"] != "" {
			content = messages["text/html; charset=UTF-8"]
		} else {
			content = messages["text/plain; charset=UTF-8"]
		}
	} else {
		content = string(m.Data)
	}
	n := Note{
		Content: string(m.Data),
		Date:    time.Now(),
	}

	// TODO(icco): Get user from email headers
	//if u := user.Current(c); u != nil {
	//	g.Author = u.String()
	//}

	// We set the same parent key on every Note entity to ensure each Note is in
	// the same entity group. Queries across the single entity group will be
	// consistent. However, the write rate to a single entity group should be
	// limited to ~1/second.
	key := datastore.NewIncompleteKey(c, "Note", noteKey(c))
	_, err := datastore.Put(c, key, &n)
	if err != nil {
		return err
	}

	return nil
}

func (m *Message) _blobstoreSave(c appengine.Context) error {
	bucketName, err := file.DefaultBucketName(c)
	if err != nil {
		return errors.New(fmt.Sprintf("failed to get default GCS bucket name: %v", err))
	}

	filename := m.ContentId
	if filename == "" {
		filename = strconv.FormatInt(time.Now().Unix(), 10)
	}

	config := google.NewAppEngineConfig(c, storage.ScopeFullControl)
	ctx := cloud.NewContext(appengine.AppID(c), &http.Client{Transport: config.NewTransport()})
	wc := storage.NewWriter(ctx, bucketName, filename, &storage.Object{
		ContentType: m.ContentType,
		Metadata:    map[string]string{},
	})

	err = storage.PutDefaultACLRule(ctx, bucketName, "allUsers", storage.RoleReader)
	if err != nil {
		c.Errorf("Unable to save default object ACL rule for bucket %q: %v", bucketName, err)
		return err
	}

	if i, err := wc.Write(m.Data); err != nil {
		c.Errorf("Unable to write data to bucket %q, file %q: %v", bucketName, filename, err)
		return err
	} else {
		c.Infof("Wrote %d bytes to bucket '%+v' and file '%+v'", i, bucketName, filename)
	}

	if err := wc.Close(); err != nil {
		c.Errorf("Unable to close bucket %q, file %q: %v", bucketName, filename, err)
		return err
	}

	// Wait for the file to be fully written.
	if _, err := wc.Object(); err != nil {
		c.Errorf("Unable to finalize file from bucket %q, file %q: %v", bucketName, filename, err)
		return err
	}

	return nil
}

func contentIdString(cid string) string {
	if cid != "" {
		cid = fmt.Sprintf("cid.%s", strings.Trim(cid, "<> "))
	}
	return cid
}
