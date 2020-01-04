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

type DisplayTransfer struct {
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
	Uploads []DisplayTransfer
	Users   []DisplayUser
}

func (s *Server) LiveHandler(w http.ResponseWriter, r *http.Request) {
	tmplPath := "web/templates/live.html"
	tmpl := template.Must(template.ParseFiles(tmplPath))
	data := liveContent{
		Users:   GetAllDisplayUsers(s.db),
		Uploads: GetAllDisplayTransfers(s.db),
	}
	err := tmpl.Execute(w, data)
	Handle(err)
}
