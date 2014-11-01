package main

import (
	"html/template"
	"io/ioutil"
	"net/http"
	"net/mail"
	"time"

	"appengine"
	"appengine/datastore"
	"appengine/user"
)

type Note struct {
	Author  string
	Content string
	Date    time.Time
}

func init() {
	http.HandleFunc("/", root)
	// http.HandleFunc("/sign", sign)
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
    <title>Go Guestbook</title>
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
    <form action="/sign" method="post">
      <div><textarea name="content" rows="3" cols="60"></textarea></div>
      <div><input type="submit" value="Sign Guestbook"></div>
    </form>
  </body>
</html>
`))

func sign(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	g := Note{
		Content: r.FormValue("content"),
		Date:    time.Now(),
	}
	if u := user.Current(c); u != nil {
		g.Author = u.String()
	}
	// We set the same parent key on every Greeting entity to ensure each Greeting
	// is in the same entity group. Queries across the single entity group
	// will be consistent. However, the write rate to a single entity group
	// should be limited to ~1/second.
	key := datastore.NewIncompleteKey(c, "Note", noteKey(c))
	_, err := datastore.Put(c, key, &g)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func incomingMail(w http.ResponseWriter, r *http.Request) {
	c := appengine.NewContext(r)
	defer r.Body.Close()
	parsed, err := mail.ReadMessage(r.Body)
	if err != nil {
		c.Errorf("Error parsing mail: %v", err)
		return
	}
	body, err := ioutil.ReadAll(parsed.Body)
	if err != nil {
		c.Errorf("Failed reading body: %v", err)
		return
	}
	c.Infof("Parsed mail: headers: %+v. body: %+v", parsed.Header, string(body))
}
