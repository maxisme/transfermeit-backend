package main

import (
	"database/sql"
	"github.com/go-sql-driver/mysql"
	"github.com/patrickmn/go-cache"
	"log"
	"os"
	"path"
	"time"
)

var GLOBALMAXFILESIZEMB = 5000
var (
	FREEFILEUPLOAD = MegabytesToBytes(250)
	FREEBANDWIDTH  = FREEFILEUPLOAD * 10
	CREDITSTEPS    = 0.5
)
var pendingSocketMessages = map[string][]SocketMessage{}

var USERDIRLEN = 50
var FILEDIR = os.Getenv("file_dir")

type Upload struct {
	ID       int       `json:"-"`
	FilePath string    `json:"file_path"`
	Size     int       `json:"file_size"`
	from     User      `json:"-"`
	to       User      `json:"-"`
	hash     string    `json:"-"`
	password string    `json:"-"`
	expiry   time.Time `json:"-"`
}

func GetUploadPassword(db *sql.DB, upload Upload) string {
	var password string
	result := db.QueryRow(`
	SELECT password
	FROM upload
	WHERE finished_dttm IS NULL
	AND to_UUID = ?
	AND file_path = ?
	AND file_hash = ?`, Hash(upload.to.UUID), upload.FilePath, upload.hash)
	if err := result.Scan(&password); err != nil {
		return ""
	}
	return password
}

// check user isn't already uploading a file to the same friend
func IsAlreadyUploading(db *sql.DB, upload *Upload) bool {
	result := db.QueryRow(`
	SELECT file_path
    FROM upload
    WHERE from_UUID = ?
    AND to_UUID = ?
    AND finished_dttm IS NULL
    LIMIT 1`, Hash(upload.from.UUID), Hash(upload.to.UUID))
	err := result.Scan(&upload.FilePath)
	return err == nil
}

// UUID and path are currently waiting to be downloaded
func AllowedToDownloadPath(db *sql.DB, user User, filePath string) bool {
	var id int
	result := db.QueryRow(`
	SELECT id
    FROM upload
	WHERE to_UUID = ?
	AND file_path = ?
    AND finished_dttm IS NULL`, Hash(user.UUID), filePath)
	_ = result.Scan(&id)
	if id > 0 {
		return true
	}
	return false
}

func InsertUpload(db *sql.DB, upload Upload) int {
	res, err := db.Exec(`
	INSERT into upload (from_UUID, to_UUID)
	VALUES (?, ?)`, Hash(upload.from.UUID), Hash(upload.to.UUID))
	Handle(err)
	ID, err := res.LastInsertId()
	Handle(err)
	return int(ID)
}

func UpdateUpload(db *sql.DB, upload Upload) error {
	return UpdateErr(db.Exec(`
	UPDATE upload set size=?, file_hash=?, file_path=?, password=?, expiry_dttm=?, updated_dttm=NOW()
	WHERE id=?`, upload.Size, upload.hash, upload.FilePath, upload.password, upload.expiry, upload.ID))
}

func CompleteUpload(db *sql.DB, upload Upload, failed bool, expired bool) {
	_, err := db.Exec(`
	UPDATE upload 
	SET file_path = NULL, finished_dttm = NOW(), password = NULL, file_hash = NULL, failed = ?
	WHERE from_UUID = ?
	AND to_UUID = ?
	AND file_path = ?`, failed, Hash(upload.from.UUID), Hash(upload.to.UUID), upload.FilePath)
	Handle(err)

	go DeleteDir(upload.FilePath)

	mess := Message{
		Title:   "Successful Transfer",
		Message: "Your friend successfully downloaded the file!",
	}
	if expired {
		mess.Title = "Expired Transfer!"
		mess.Message = "The file you uploaded has expired."
	} else if failed {
		mess.Title = "Unsuccessful Transfer"
		mess.Message = "Your friend may have ignored the download!"
	}

	go SendSocketMessage(SocketMessage{Message: &mess}, upload.from.UUID, true)
}

// remove uploaded directory
func DeleteDir(filePath string) bool {
	dir := path.Dir(FILEDIR + filePath)
	if err := os.RemoveAll(dir); err != nil {
		_ = os.Remove(dir)
		Handle(err)
		return false
	}
	return true
}

// remove all expired uploads
func (s *Server) CleanIncompleteUploads() {
	log.Println("STARTED CLEANUP")

	rows, err := s.db.Query(`
	SELECT 
		upload.id,
		upload.file_path, 
		upload.to_UUID,
		upload.from_UUID
	FROM upload
	JOIN user ON upload.from_UUID = user.UUID
	WHERE upload.finished_dttm IS NULL
	AND upload.expiry_dttm IS NOT NULL AND upload.expiry_dttm > NOW()
	AND (upload.updated_dttm IS NULL OR upload.updated_dttm + interval 1 minute <= NOW())`)
	Handle(err)

	for rows.Next() {
		var upload Upload
		err := rows.Scan(&upload.ID, &upload.FilePath, &upload.to.UUID, &upload.from.UUID)
		Handle(err)
		go CompleteUpload(s.db, upload, true, true)
	}

	log.Println("FINISHED CLEANUP")
}

func FetchAllDisplayUploads(db *sql.DB) []DisplayUpload {
	var uploads []DisplayUpload
	if u, found := c.Get("uploads"); found {
		uploads = u.([]DisplayUpload)
	} else {
		log.Println("Refreshed upload cache")
		// fetch uploads from db if not in cache
		rows, err := db.Query(`
		SELECT from_UUID, to_UUID, expiry_dttm, size, file_hash, failed, updated_dttm, finished_dttm
		FROM upload`)
		defer rows.Close()
		Handle(err)
		for rows.Next() {
			var u DisplayUpload
			var fs int
			var updated mysql.NullTime
			var finished mysql.NullTime
			err = rows.Scan(&u.FromUUID, &u.ToUUID, &u.FileExpiry, &fs, &u.FileHash, &u.Failed, &updated, &finished)
			Handle(err)
			u.Downloading = updated.Valid
			u.Finished = finished.Valid
			u.FileSize = BytesToReadable(fs)
			uploads = append(uploads, u)
		}
		c.Set("uploads", uploads, cache.DefaultExpiration)
	}
	return uploads
}
