package main

import (
	"database/sql"
	"github.com/go-sql-driver/mysql"
	"github.com/patrickmn/go-cache"
	"log"
	"os"
	"path"
	"strconv"
	"sync"
	"time"
)

var GLOBALMAXFILESIZEMB = 5000
var (
	FREEFILEUPLOAD = MegabytesToBytes(250)
	FREEBANDWIDTH  = FREEFILEUPLOAD * 10
	CREDITSTEPS    = 0.5
)
var (
	pendingSocketMessages = map[string][]SocketMessage{}
	pendingSocketMutex    = sync.RWMutex{}
)
var USERDIRLEN = 50
var FILEDIR = os.Getenv("file_dir")

type Transfer struct {
	ID       int       `json:"-"`
	FilePath string    `json:"file_path"`
	Size     int       `json:"file_size"`
	from     User      `json:"-"`
	to       User      `json:"-"`
	hash     string    `json:"-"`
	password string    `json:"-"`
	expiry   time.Time `json:"-"`
}

func GetTransferPasswordAndUUID(db *sql.DB, transfer Transfer) (password string, UUID string) {
	result := db.QueryRow(`
	SELECT password, from_UUID
	FROM transfer
	WHERE finished_dttm IS NULL
	AND to_UUID = ?
	AND file_path = ?
	AND file_hash = ?`, Hash(transfer.to.UUID), transfer.FilePath, transfer.hash)
	if err := result.Scan(&password, &UUID); err != nil {
		Handle(err)
		return "", ""
	}
	return password, UUID
}

// check user isn't already transferring a file to the same friend
func IsAlreadyTransferring(db *sql.DB, transfer *Transfer) bool {
	result := db.QueryRow(`
	SELECT file_path
    FROM transfer
    WHERE from_UUID = ?
    AND to_UUID = ?
    AND finished_dttm IS NULL
    LIMIT 1`, Hash(transfer.from.UUID), Hash(transfer.to.UUID))
	err := result.Scan(&transfer.FilePath)
	return err == nil
}

// UUID and path are currently waiting to be downloaded
func AllowedToDownloadPath(db *sql.DB, user User, filePath string) bool {
	var id int
	result := db.QueryRow(`
	SELECT id
    FROM transfer
	WHERE to_UUID = ?
	AND file_path = ?
    AND finished_dttm IS NULL`, Hash(user.UUID), filePath)
	_ = result.Scan(&id)
	if id > 0 {
		return true
	}
	return false
}

func InsertTransfer(db *sql.DB, transfer Transfer) int {
	res, err := db.Exec(`
	INSERT into transfer (from_UUID, to_UUID)
	VALUES (?, ?)`, Hash(transfer.from.UUID), Hash(transfer.to.UUID))
	Handle(err)
	ID, err := res.LastInsertId()
	Handle(err)
	return int(ID)
}

func UpdateTransfer(db *sql.DB, transfer Transfer) error {
	return UpdateErr(db.Exec(`
	UPDATE transfer set size=?, file_hash=?, file_path=?, password=?, expiry_dttm=?, updated_dttm=NOW()
	WHERE id=?`, transfer.Size, transfer.hash, transfer.FilePath, transfer.password, transfer.expiry, transfer.ID))
}

func KeepAliveTransfer(db *sql.DB, user User, path string) error {
	return UpdateErr(db.Exec(`
	UPDATE transfer set size=updated_dttm=NOW()
	WHERE file_path=?
	AND to_UUID
	OR from_UUID`, path, Hash(user.UUID), Hash(user.UUID)))
}

func CompleteTransfer(db *sql.DB, transfer Transfer, failed bool, expired bool) {
	err := UpdateErr(db.Exec(`
	UPDATE transfer 
	SET file_path = NULL, finished_dttm = NOW(), password = NULL, failed = ?
	WHERE from_UUID = ?
	AND to_UUID = ?
	AND file_path = ?`, failed, Hash(transfer.from.UUID), Hash(transfer.to.UUID), transfer.FilePath))
	Handle(err)

	go DeleteUploadDir(transfer.FilePath)

	mess := DesktopMessage{
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

	go SendSocketMessage(SocketMessage{Message: &mess}, transfer.from.UUID, true)
}

// remove uploaded directory
func DeleteUploadDir(filePath string) bool {
	dir := path.Dir(FILEDIR + filePath)
	if err := os.RemoveAll(dir); err != nil {
		_ = os.Remove(dir)
		Handle(err)
		return false
	}
	return true
}

func (s *Server) CleanIncompleteTransfers() {
	log.Println("STARTED TRANSFER CLEANUP")

	rows, err := s.db.Query(`
	SELECT id, file_path, to_UUID, from_UUID
	FROM transfer
	WHERE finished_dttm IS NULL
	AND expiry_dttm IS NOT NULL
	AND expiry_dttm < NOW()
	AND (updated_dttm IS NULL OR updated_dttm + interval 1 minute > NOW())`)
	Handle(err)

	cnt := 0
	for rows.Next() {
		var transfer Transfer
		err := rows.Scan(&transfer.ID, &transfer.FilePath, &transfer.to.UUID, &transfer.from.UUID)
		Handle(err)
		go CompleteTransfer(s.db, transfer, true, true)
		cnt += 1
	}

	log.Println("FINISHED CLEANUP - deleted " + strconv.Itoa(cnt) + " rows")
}

func FetchAllDisplayTransfers(db *sql.DB) []DisplayTransfer {
	var transfers []DisplayTransfer
	if u, found := c.Get("transfers"); found {
		transfers = u.([]DisplayTransfer)
	} else {
		log.Println("Refreshed transfer cache")
		// fetch transfers from db if not in cache
		rows, err := db.Query(`
		SELECT from_UUID, to_UUID, expiry_dttm, size, file_hash, failed, updated_dttm, finished_dttm
		FROM transfer`)
		defer rows.Close()
		Handle(err)
		for rows.Next() {
			var u DisplayTransfer
			var fs int
			var updated mysql.NullTime
			var finished mysql.NullTime
			err = rows.Scan(&u.FromUUID, &u.ToUUID, &u.FileExpiry, &fs, &u.FileHash, &u.Failed, &updated, &finished)
			Handle(err)
			u.Downloading = updated.Valid
			u.Finished = finished.Valid
			u.FileSize = BytesToReadable(fs)
			transfers = append(transfers, u)
		}
		c.Set("transfers", transfers, cache.DefaultExpiration)
	}
	return transfers
}
