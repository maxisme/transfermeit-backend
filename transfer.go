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

const (
	maxFileUploadSizeMB = 5000
	freeFileUploadBytes = 250000000
	freeBandwidthBytes  = freeFileUploadBytes * 10

	creditSteps = 0.5
	userDirLen  = 50
)

var (
	pendingSocketMessages = map[string][]SocketMessage{}
	pendingSocketMutex    = sync.RWMutex{}
)

var fileStoreDirectory = os.Getenv("file_dir")

// Transfer structure
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

// GetPasswordAndUUID fetches the password for the transfer and the UUID of the sending user
// based on the UUID of the destination user the filepath of the transfer and the file hash
func (transfer *Transfer) GetPasswordAndUUID(db *sql.DB) {
	result := db.QueryRow(`
	SELECT password, from_UUID
	FROM transfer
	WHERE finished_dttm IS NULL
	AND to_UUID = ?
	AND file_path = ?
	AND file_hash = ?`, Hash(transfer.to.UUID), transfer.FilePath, transfer.hash)
	Handle(result.Scan(&transfer.password, &transfer.from.UUID))
}

// GetLiveFilePath fetches the file path between two users
func (transfer *Transfer) GetLiveFilePath(db *sql.DB) {
	result := db.QueryRow(`
	SELECT file_path
    FROM transfer
    WHERE from_UUID = ?
    AND to_UUID = ?
    AND finished_dttm IS NULL
    LIMIT 1`, Hash(transfer.from.UUID), Hash(transfer.to.UUID))
	_ = result.Scan(&transfer.FilePath)
}

// InitialStore stores the from_UUID and to_UUID in the transfer table as placeholders
func (transfer Transfer) InitialStore(db *sql.DB) int {
	res, err := db.Exec(`
	INSERT into transfer (from_UUID, to_UUID)
	VALUES (?, ?)`, Hash(transfer.from.UUID), Hash(transfer.to.UUID))
	Handle(err)
	ID, err := res.LastInsertId()
	Handle(err)
	return int(ID)
}

// Store stores the full information of the transfer based on the ID from InitialStore
func (transfer Transfer) Store(db *sql.DB) error {
	return UpdateErr(db.Exec(`
	UPDATE transfer 
	SET size=?, file_hash=?, file_path=?, password=?, expiry_dttm=?, updated_dttm=NOW()
	WHERE id=?`, transfer.Size, transfer.hash, transfer.FilePath, transfer.password, transfer.expiry, transfer.ID))
}

// KeepAliveTransfer will update the updated_dttm of the transfer to prevent the cleanup CleanExpiredTransfers()
// from executing while still downloading
func KeepAliveTransfer(db *sql.DB, user User, path string) {
	Handle(UpdateErr(db.Exec(`
	UPDATE transfer 
	SET updated_dttm=NOW()
	WHERE file_path=?
	AND to_UUID
	OR from_UUID`, path, Hash(user.UUID), Hash(user.UUID))))
}

// Completed will mark a transfer as completed and return the state back to the user over socket message.
func (transfer Transfer) Completed(db *sql.DB, failed bool, expired bool) {
	err := UpdateErr(db.Exec(`
	UPDATE transfer 
	SET file_path = NULL, finished_dttm = NOW(), password = NULL, failed = ?
	WHERE from_UUID = ?
	AND to_UUID = ?
	AND file_path = ?`, failed, Hash(transfer.from.UUID), Hash(transfer.to.UUID), transfer.FilePath))
	Handle(err)

	go deleteUploadDir(transfer.FilePath)

	message := DesktopMessage{}
	if expired {
		message.Title = "Expired Transfer!"
		message.Message = "Your file was not downloaded in time!"
	} else if failed {
		message.Title = "Unsuccessful Transfer"
		message.Message = "Your friend may have ignored the transfer!"
	} else {
		message.Title = "Successful Transfer"
		message.Message = "Your friend has received your file!"

		// send user stats update to sender
		fromUser := User{UUID: transfer.from.UUID}
		fromUser.GetStats(db)
		SendSocketMessage(SocketMessage{
			User: &fromUser,
		}, transfer.from.UUID, true)
	}

	SendSocketMessage(SocketMessage{Message: &message}, transfer.from.UUID, true)
}

// AllowedToDownload verifies that the download request is legitimate
func AllowedToDownload(db *sql.DB, user User, filePath string) bool {
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

// CleanExpiredTransfers removes transfers which have exceeded the length of time they are allowed to be hosted on the
// server
func (s *Server) CleanExpiredTransfers() {
	// find and remove all expired uploads
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
		go transfer.Completed(s.db, true, true)
		cnt += 1
	}
	rows.Close()

	if cnt > 0 {
		log.Println("Deleted " + strconv.Itoa(cnt) + " transfers")
	}
}

func getAllDisplayTransfers(db *sql.DB) []displayTransfer {
	var transfers []displayTransfer
	if u, found := c.Get("transfers"); found {
		transfers = u.([]displayTransfer)
	} else {
		log.Println("Refreshed transfer cache")
		// fetch transfers from db if not in cache
		rows, err := db.Query(`
		SELECT from_UUID, to_UUID, expiry_dttm, size, file_hash, failed, updated_dttm, finished_dttm
		FROM transfer`)
		defer rows.Close()
		Handle(err)
		for rows.Next() {
			var (
				dt       displayTransfer
				fileSize int
				updated  mysql.NullTime
				finished mysql.NullTime
			)
			err = rows.Scan(&dt.FromUUID, &dt.ToUUID, &dt.FileExpiry, &fileSize, &dt.FileHash, &dt.Failed, &updated, &finished)
			Handle(err)
			dt.Downloading = updated.Valid
			dt.Finished = finished.Valid
			dt.FileSize = BytesToReadable(fileSize)
			transfers = append(transfers, dt)
		}
		c.Set("transfers", transfers, cache.DefaultExpiration)
	}
	return transfers
}

func deleteUploadDir(filePath string) bool {
	dir := path.Dir(fileStoreDirectory + filePath)
	if err := os.RemoveAll(dir); err != nil {
		_ = os.Remove(dir)
		Handle(err)
		return false
	}
	return true
}
