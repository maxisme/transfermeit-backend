package main

import (
	"database/sql"
	"github.com/go-sql-driver/mysql"
	"github.com/patrickmn/go-cache"
	"html/template"

	log "github.com/sirupsen/logrus"
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

func getAllDisplayTransfers(db *sql.DB) ([]displayTransfer, error) {
	var transfers []displayTransfer
	if u, found := c.Get("transfers"); found {
		transfers = u.([]displayTransfer)
	} else {
		log.Info("Refreshed transfer cache")
		// fetch transfers from db if not in cache
		rows, err := db.Query(`
		SELECT from_UUID, to_UUID, expiry_dttm, size, file_hash, failed, updated_dttm, finished_dttm
		FROM transfer`)
		if err != nil {
			return nil, err
		}

		defer rows.Close()
		for rows.Next() {
			var (
				dt       displayTransfer
				fileSize int64
				updated  mysql.NullTime
				finished mysql.NullTime
			)
			err = rows.Scan(&dt.FromUUID, &dt.ToUUID, &dt.FileExpiry, &fileSize, &dt.FileHash, &dt.Failed, &updated, &finished)
			if err != nil {
				return nil, err
			}

			dt.Downloading = updated.Valid
			dt.Finished = finished.Valid
			dt.FileSize = BytesToReadable(fileSize)
			transfers = append(transfers, dt)
		}
		c.Set("transfers", transfers, cache.DefaultExpiration)
	}
	return transfers, nil
}

// LiveHandler returns a page displaying all historic transfers
func (s *Server) LiveHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		WriteError(w, r, http.StatusBadRequest, "Invalid method")
		return
	}
	tmplPath := "web/templates/live.html"
	tmpl := template.Must(template.ParseFiles(tmplPath))

	// get users
	users, err := getAllDisplayUsers(s.db)
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// get uploads
	uploads, err := getAllDisplayTransfers(s.db)
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	data := liveContent{
		Users:   users,
		Uploads: uploads,
	}
	if err := tmpl.Execute(w, data); err != nil {
		WriteError(w, r, http.StatusBadRequest, err.Error())
		return
	}
}
