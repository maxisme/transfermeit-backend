package main

import (
	"database/sql"
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
	from     User      `json:"-"`
	to       User      `json:"-"`
	size     int       `json:"-"`
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
	WHERE id=?`, upload.size, upload.hash, upload.FilePath, upload.password, upload.expiry, upload.ID))
}

func CompleteUpload(db *sql.DB, upload Upload, failed bool, expired bool) {
	if !expired {
		// hash UUIDs
		upload.from.UUID = Hash(upload.from.UUID)
		upload.to.UUID = Hash(upload.to.UUID)
	}

	_, err := db.Exec(`
	UPDATE upload 
	SET to_uuid = NULL, /* <-- for privacy */
	file_path = NULL, finished_dttm = NOW(), password = NULL, file_hash = NULL, failed = ?
	WHERE from_UUID = ?
	AND to_UUID = ?
	AND file_path = ?`, failed, upload.from.UUID, upload.to.UUID, upload.FilePath)
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

	go SendSocketMessage(SocketMessage{Message: &mess}, upload.to.UUID, true)
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
