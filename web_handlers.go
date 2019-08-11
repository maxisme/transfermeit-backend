package main

import (
	"html/template"
	"net/http"
	"time"
)

type DisplayUser struct {
	UUID      string
	PubKey    string
	Connected bool
}

type DisplayUpload struct {
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
	Uploads []DisplayUpload
	Users   []DisplayUser
}

func (s *Server) liveHandler(w http.ResponseWriter, r *http.Request) {
	tmplPath := "web/templates/live.html"
	tmpl := template.Must(template.ParseFiles(tmplPath))
	data := liveContent{
		Users:   FetchAllDisplayUsers(s.db),
		Uploads: FetchAllDisplayUploads(s.db),
	}
	err := tmpl.Execute(w, data)
	Handle(err)
}
