package main

import (
	"context"
	"database/sql"
	"fmt"
	tdb "github.com/maxisme/transfermeit-backend/tracer/db"
	tminio "github.com/maxisme/transfermeit-backend/tracer/minio"
	"github.com/minio/minio-go/v7"
	log "github.com/sirupsen/logrus"
	"net/http"
	"time"
)

const (
	maxFileUploadSizeMB = 5000
	freeFileUploadBytes = 250000000
	freeBandwidthBytes  = freeFileUploadBytes * 10

	creditSteps = 0.5
	userDirLen  = 50
)

// Transfer structure
type Transfer struct {
	ID         int64     `json:"-"`
	ObjectName string    `json:"object_name"`
	Size       int64     `json:"file_size"`
	from       User      `json:"-"`
	to         User      `json:"-"`
	hash       string    `json:"-"`
	password   string    `json:"-"`
	expiry     time.Time `json:"-"`
}

// GetPasswordAndUUID fetches the password for the transfer and the UUID of the sending user
// based on the UUID of the destination user the filepath of the transfer and the file hash
func (transfer *Transfer) GetPasswordAndUUID(r *http.Request, db *sql.DB) error {
	result := tdb.QueryRow(r, db, `
	SELECT password, from_UUID
	FROM transfer
	WHERE finished_dttm IS NULL
	AND to_UUID = ?
	AND object_name = ?`, Hash(transfer.to.UUID), transfer.ObjectName)
	return result.Scan(&transfer.password, &transfer.from.UUID)
}

// AlreadyToUser returns true if already transferring between two users
func (transfer *Transfer) AlreadyToUser(r *http.Request, db *sql.DB) bool {
	var id int64
	result := tdb.QueryRow(r, db, `
	SELECT id
    FROM transfer
    WHERE from_UUID = ?
    AND to_UUID = ?
    AND finished_dttm IS NULL`, Hash(transfer.from.UUID), Hash(transfer.to.UUID))
	_ = result.Scan(&id)
	return id > 0
}

// InitialStore stores the from_UUID and to_UUID in the transfer table as placeholders
func (transfer Transfer) InitialStore(r *http.Request, db *sql.DB) (int64, error) {
	res, err := tdb.Exec(r, db, `
	INSERT into transfer (from_UUID, to_UUID)
	VALUES (?, ?)`, Hash(transfer.from.UUID), Hash(transfer.to.UUID))
	if err != nil {
		return 0, err
	}
	ID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return ID, nil
}

// Store stores the full information of the transfer based on the ID from InitialStore
func (transfer Transfer) Store(r *http.Request, db *sql.DB) error {
	return UpdateErr(tdb.Exec(r, db, `UPDATE transfer 
	SET size=?, object_name=?, password=?, expiry_dttm=?, updated_dttm=NOW()
	WHERE id=?`, transfer.Size, transfer.ObjectName, transfer.password, transfer.expiry, transfer.ID))
}

// KeepAliveTransfer will update the updated_dttm of the transfer to prevent the cleanup CleanExpiredTransfers()
// from executing while still downloading
func KeepAliveTransfer(r *http.Request, db *sql.DB, user User, objectName string) error {
	return UpdateErr(tdb.Exec(r, db, `
	UPDATE transfer 
	SET updated_dttm=NOW()
	WHERE object_name=?
	AND to_UUID = ?`, objectName, Hash(user.UUID)))
}

// Completed will mark a transfer as completed and return the state back to the user over socket message.
func (transfer Transfer) Completed(r *http.Request, s *Server, failed bool, expired bool) error {
	var f int8
	if failed {
		f = 1
	}
	err := UpdateErr(tdb.Exec(r, s.db, `
	UPDATE transfer 
	SET object_name = '', finished_dttm = NOW(), password = NULL, failed = ?
	WHERE from_UUID = ?
	AND to_UUID = ?
	AND object_name = ?`, f, Hash(transfer.from.UUID), Hash(transfer.to.UUID), transfer.ObjectName))
	if err != nil {
		return err
	}

	if transfer.ObjectName != "" {
		err = tminio.RemoveObject(r, s.minio, context.Background(), bucketName, transfer.ObjectName,
			minio.RemoveObjectOptions{})
		if err != nil {
			return err
		}
	}

	message := DesktopMessage{}
	if expired {
		message.Title = "Expired Transfer!"
		message.Message = "Your file was not downloaded in time!"
	} else if failed {
		message.Title = "Cancelled Transfer"
		message.Message = "Your friend may have ignored the transfer!"
	} else {
		message.Title = "Successful Transfer"
		message.Message = "Your friend has received your file!"

		// send user stats update to sender
		fromUser := User{UUID: transfer.from.UUID}
		fromUser.Stats(r, s.db)
		if err := s.funnels.Send(s.redis, Hash(transfer.from.UUID), SocketMessage{
			User: &fromUser,
		}); err != nil {
			return err
		}
	}

	return s.funnels.Send(s.redis, Hash(transfer.from.UUID), SocketMessage{Message: &message})
}

// AllowedToDownload verifies that the download request is legitimate
func AllowedToDownload(r *http.Request, db *sql.DB, user User, objectName string) bool {
	var id int
	result := tdb.QueryRow(r, db, `
	SELECT id
    FROM transfer
	WHERE to_UUID = ?
	AND object_name = ?
    AND finished_dttm IS NULL`, Hash(user.UUID), objectName)
	_ = result.Scan(&id)
	if id > 0 {
		return true
	}
	return false
}

// CleanExpiredTransfers removes transfers which have exceeded the length of time they are allowed to be hosted on the
// server
func (s *Server) CleanExpiredTransfers(r *http.Request) error {
	// find and remove all expired uploads
	rows, err := tdb.Query(r, s.db, `
	SELECT id, object_name, to_UUID, from_UUID
	FROM transfer
	WHERE finished_dttm IS NULL AND expiry_dttm IS NOT NULL
  	AND ((expiry_dttm < NOW() AND updated_dttm IS NULL) 
           OR (updated_dttm IS NOT NULL AND updated_dttm + interval 1 minute <= NOW()))`)
	if err != nil {
		return err
	}

	cnt := 0
	for rows.Next() {
		var transfer Transfer
		err := rows.Scan(&transfer.ID, &transfer.ObjectName, &transfer.to.UUID, &transfer.from.UUID)
		if err != nil {
			return err
		}
		if err := transfer.Completed(r, s, true, true); err != nil {
			return err
		}
		cnt += 1
	}
	rows.Close()

	if cnt > 0 {
		Log(nil, log.InfoLevel, fmt.Sprintf("Deleted %d transfers", cnt))
	}
	return nil
}
