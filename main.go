package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"net/http"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"

	"appengine"
	"appengine/datastore"
	"appengine/file"
	"appengine/user"

	"github.com/golang/oauth2/google"
	"google.golang.org/cloud"
	"google.golang.org/cloud/storage"
)

type Note struct {
	Author  user.User
	Content template.HTML
	Date    time.Time
}

type UserData struct {
	Hash string
	User user.User
}

func init() {
	http.HandleFunc("/", root)
	http.HandleFunc("/_ah/mail/", incomingMail)
}

// noteKey returns the key used for all note entries.
func noteKey(c appengine.Context) *datastore.Key {
	return datastore.NewKey(c, "Note", "notes", 0, nil)
}

func GetUserByHash(c appengine.Context, hash string) *user.User {
	data := &UserData{}
	q := datastore.NewQuery("UserData").Filter("Hash =", hash).Limit(1)
	_, err := q.Run(c).Next(data)
	if err != nil {
		c.Errorf("While getting UserHash: %+v", err)
		return nil
	}

	return &data.User
}

func GetUserHash(c appengine.Context, u *user.User) string {
	data := &UserData{}
	k := datastore.NewKey(c, "UserData", u.String(), 0, nil)
	err := datastore.Get(c, k, data)
	if err != nil {
		c.Errorf("While getting UserHash: %+v", err)
	}

	if data.Hash == "" {
		data = &UserData{}
		data.User = *u
		b := make([]byte, 12)
		_, err := rand.Read(b)
		if err != nil {
			c.Errorf("While generating bytes: %+v", err)
			return ""
		}
		data.Hash = hex.EncodeToString(b)
		_, err = datastore.Put(c, k, data)
		if err != nil {
			c.Warningf("Error writing UserData (%+v -> %+v): %+v", k, data, err)
		}
	}

	return data.Hash
}

func root(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	u := user.Current(c)
	if u == nil {
		url, _ := user.LoginURL(c, "/")
		http.Redirect(w, r, url, http.StatusFound)
		return
	}

	q := datastore.NewQuery("Note").Ancestor(noteKey(c)).Filter("Author.Email =", u.String()).Order("-Date").Limit(10)
	notes := make([]Note, 0, 10)
	if _, err := q.GetAll(c, &notes); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := noteTemplate.Execute(w, &RootTemplateData{Notes: notes, Hash: GetUserHash(c, u)}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type RootTemplateData struct {
	Notes []Note
	Hash  string
}

// TODO(icco): Move to seperate file.
var noteTemplate = template.Must(template.New("book").Parse(`
<html>
  <head>
    <title>Nat Notes</title>
  </head>
  <body>
    <p>Your email target: {{.Hash}}@natwelch-what.appspotmail.com</p>

    {{range .Notes}}
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

	// Make sure this is an address we know about
	addrs, err := parsed.Header.AddressList("To")
	if err != nil {
		c.Errorf("Failed reading FROM: %v", err)
		return
	}
	userHash := strings.Split(addrs[0].Address, "@")[0]
	user := GetUserByHash(c, userHash)
	if user == nil {
		c.Errorf("Not a valid hash: %s", userHash)
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
		if err := v.Store(c, user); err != nil {
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
func (m *Message) Store(c appengine.Context, user *user.User) error {
	if strings.HasPrefix(m.ContentType, "text/") || strings.HasPrefix(m.ContentType, "multipart/alternative") {
		return m._datastoreSave(c, user)
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

func (m *Message) _datastoreSave(c appengine.Context, user *user.User) error {
	mediaType, params, err := mime.ParseMediaType(m.ContentType)
	if err != nil {
		return err
	}

	content := ""
	if strings.HasPrefix(mediaType, "multipart/") {
		messages := map[string]string{}
		mr := multipart.NewReader(bytes.NewBuffer(m.Data), params["boundary"])
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			} else if err != nil {
				return err
			}

			slurp, err := ioutil.ReadAll(p)
			if err != nil {
				return err
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

	content = replaceCidWithUrl(content)
	n := Note{
		Content: template.HTML(content),
		Date:    time.Now(),
		Author:  *user,
	}

	// We set the same parent key on every Note entity to ensure each Note is in
	// the same entity group. Queries across the single entity group will be
	// consistent. However, the write rate to a single entity group should be
	// limited to ~1/second.
	key := datastore.NewIncompleteKey(c, "Note", noteKey(c))
	_, err = datastore.Put(c, key, &n)
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

	if data, err := base64.StdEncoding.DecodeString(string(m.Data)); err != nil {
		c.Errorf("Unable to decode string: %v", err)
	} else {
		if i, err := wc.Write(data); err != nil {
			c.Errorf("Unable to write data to bucket %q, file %q: %v", bucketName, filename, err)
			return err
		} else {
			c.Infof("Wrote %d bytes to bucket '%+v' and file '%+v'", i, bucketName, filename)
		}
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

func replaceCidWithUrl(html string) string {
	r := regexp.MustCompile("cid:")
	return r.ReplaceAllString(html, "http://storage.googleapis.com/natwelch-what.appspot.com/cid.")
}

func contentIdString(cid string) string {
	if cid != "" {
		cid = fmt.Sprintf("cid.%s", strings.Trim(cid, "<> "))
	}
	return cid
}
