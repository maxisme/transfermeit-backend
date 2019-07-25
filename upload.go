package main

import (
	"database/sql"
	"fmt"
	"os"
	"path"
)

var GLOBALMAXFILESIZEMB = 5000
var (
	FREEBANDWIDTH  = MegabytesToBytes(2500)
	FREEFILEUPLOAD = MegabytesToBytes(250)
	CREDITSTEPS    = 0.5
)

var USERDIRLEN = 50
var FILEDIR = os.Getenv("file_dir")

type Upload struct {
	ID       int    `json:"-"`
	FilePath string `json:"file_path"`
	size     int    `json:"-"`
	fromUUID string `json:"-"`
	toUUID   string `json:"-"`
	hash     string `json:"-"`
	password string `json:"-"`
	pro      bool   `json:"-"`
}

func GetUploadPassword(db *sql.DB, upload Upload) string {
	var password string
	result := db.QueryRow(`
	SELECT password
	FROM upload
	WHERE finished_dttm IS NULL
	AND to_UUID = ?
	AND file_path = ?
	AND file_hash = ?`, Hash(upload.toUUID), upload.FilePath, upload.hash)
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
    LIMIT 1`, upload.fromUUID, upload.toUUID)
	err := result.Scan(&upload.FilePath)
	return err == nil
}

// UUID and path are currently waiting to be downloaded
func AllowedToDownloadPath(db *sql.DB, UUID string, filePath string) bool {
	var id int
	result := db.QueryRow(`
	SELECT id
    FROM upload
	WHERE to_UUID = ?
	AND file_path = ?
    AND finished_dttm IS NULL`, Hash(UUID), filePath)
	_ = result.Scan(&id)
	if id > 0 {
		return true
	}
	return false
}

func InsertUpload(db *sql.DB, upload Upload) int {
	res, err := db.Exec(`
	INSERT into upload (from_UUID, to_UUID, file_path, is_pro)
	VALUES (?, ?, ?, ?)`, upload.fromUUID, upload.toUUID, upload.FilePath, upload.pro)
	Err(err)
	ID, err := res.LastInsertId()
	Err(err)
	return int(ID)
}

func UpdateUpload(db *sql.DB, upload Upload) {
	_, err := db.Exec(`
	UPDATE upload set size=?, file_hash=?, file_path=?, password=?, updated_dttm=NOW()
	WHERE id=?`, upload.size, upload.hash, upload.FilePath, upload.password, upload.ID)
	Err(err)
}

func CompleteUpload(db *sql.DB, upload Upload, failed bool) {
	if DeleteDir(upload.FilePath) {
		_, err := db.Exec(`
		UPDATE upload 
		SET to_uuid = NULL, /* <-- for privacy */
		file_path = NULL, finished_dttm = NOW(), password = NULL, 
		file_hash = NULL, failed = ?
		WHERE from_UUID = ?
		AND to_UUID = ?
		AND file_path = ?`, failed, upload.fromUUID, upload.toUUID, upload.FilePath)
		Err(err)
	}
}

// remove uploaded directory
func DeleteDir(filePath string) bool {
	dir := path.Dir(FILEDIR + filePath)
	fmt.Println(dir)
	if err := os.RemoveAll(dir); err != nil {
		_ = os.Remove(dir)
		Err(err)
		return false
	}
	return true
}

// remove all expired uploads
func (s *server) CleanIncompleteUploads() {
	rows, err := s.db.Query(`
	SELECT 
		upload.id,
		upload.file_path, 
		upload.to_UUID,
		upload.from_UUID
	FROM upload
	JOIN user ON upload.from_UUID = user.UUID
	WHERE upload.started_dttm + interval user.wanted_mins minute <= NOW()
	AND (upload.updated_dttm IS NULL OR upload.updated_dttm + interval 1 minute <= NOW())
	AND upload.finished_dttm IS NULL`)
	Err(err)

	for rows.Next() {
		var upload Upload
		err := rows.Scan(&upload.ID, &upload.FilePath, &upload.toUUID, &upload.fromUUID)
		Err(err)
		go CompleteUpload(s.db, upload, true)
	}
}
