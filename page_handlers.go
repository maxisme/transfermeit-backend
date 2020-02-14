package main

import (
	"html/template"
	"net/http"
	"time"
)

type displayUser struct {
	UUID      string
	PubKey    string
	Connected bool
}

type displayTransfer struct {
	ToUUID      string
	FromUUID    string
	FileHash    string
	FileExpiry  time.Time
	FileSize    string
	Downloading bool
	Finished    bool
	Failed      bool
}

type liveContent struct {
	Uploads []displayTransfer
	Users   []displayUser
}

// LiveHandler returns a page displaying all historic transfers
func (s *Server) LiveHandler(w http.ResponseWriter, r *http.Request) {
	tmplPath := "web/templates/live.html"
	tmpl := template.Must(template.ParseFiles(tmplPath))
	data := liveContent{
		Users:   getAllDisplayUsers(s.db),
		Uploads: getAllDisplayTransfers(s.db),
	}
	err := tmpl.Execute(w, data)
	Handle(err)
}
