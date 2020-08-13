package main

import (
	"github.com/gorilla/websocket"
	tminio "github.com/maxisme/transfermeit-backend/tracer/minio"
	"github.com/minio/minio-go/v7"
	log "github.com/sirupsen/logrus"

	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

const (
	uploadSessionName = "upload"
	fileContentType   = "application/octet-stream"
)

const (
	InvalidFileSizeErrorCode = 461
	InvalidFriendErrorCode   = 462
	SendToSelfErrorCode      = 463
	BandwidthLimitErrorCode  = 464
	FileSizeLimitErrorCode   = 465
)

// TogglePermCodeHandler either turns on or off a users perm code depending if they have one already
func (s *Server) TogglePermCodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, r, http.StatusBadRequest, "Invalid method")
		return
	}

	// fetch post data
	if err := r.ParseForm(); err != nil {
		WriteError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !user.IsValid(r, s.db) {
		Log(r, log.ErrorLevel, "Invalid credentials")
		WriteError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	user.GetTier(r, s.db)
	if user.Tier >= permUserTier {
		permCode, customCode, err := GetUserPermCode(r, s.db, user)
		if err != nil {
			WriteError(w, r, http.StatusBadRequest, err.Error())
			return
		}
		if permCode.Valid || customCode.Valid {
			// remove any stored codes (INCLUDING custom code) as they already have one or the other
			if err := RemovePermCodes(r, s.db, user); err != nil {
				WriteError(w, r, http.StatusInternalServerError, err.Error())
				return
			}
		} else {
			// turn on random perm code
			user.Code = GenCode(r, s.db)
			if err := SetPermCode(r, s.db, user); err != nil {
				WriteError(w, r, http.StatusInternalServerError, "Failed to set permanent code")
				return
			}
		}
	}

	_ = WriteJSON(w, user)
}

// CustomCodeHandler sets a users custom code
func (s *Server) CustomCodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, r, http.StatusBadRequest, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		WriteError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !user.IsValid(r, s.db) {
		WriteError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	user.Code = r.Form.Get("custom_code")
	if len(user.Code) != codeLen {
		WriteError(w, r, http.StatusBadRequest, "Invalid custom code")
		return
	}

	user.GetTier(r, s.db)
	if user.Tier >= customCodeUserTier {
		err := SetCustomCode(r, s.db, user)
		if err == nil {
			if err := WriteJSON(w, user); err != nil {
				WriteError(w, r, http.StatusInternalServerError, err.Error())
				return
			}
			return
		} else {
			WriteError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
	}
	WriteError(w, r, 402, "Failed to set activation code")
}

// RegisterCreditHandler will associate a credit code to an account
func (s *Server) RegisterCreditHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, r, http.StatusBadRequest, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		WriteError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !user.IsValid(r, s.db) {
		WriteError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	creditCode := r.Form.Get("credit_code")
	if len(creditCode) == CreditCodeLen {
		if err := SetCreditCode(r, s.db, user, creditCode); err != nil {
			WriteError(w, r, http.StatusInternalServerError, "Failed to register credit")
			return
		}
	}
}

// CreateCodeHandler creates an account and/or updates a users code
func (s *Server) CreateCodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, r, http.StatusBadRequest, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		WriteError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	user := User{}
	user.UUID = r.Form.Get("UUID")
	user.UUIDKey = r.Form.Get("UUID_key")
	user.PublicKey = r.Form.Get("public_key")

	if user.UUID == "" {
		WriteError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	if user.PublicKey == "" {
		WriteError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	if err := IsValidPublicKey(user.PublicKey); err != nil {
		WriteError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	wantedMins, err := strconv.Atoi(r.Form.Get("wanted_mins")) // convert to int
	if err != nil {
		wantedMins = defaultAccountLifeMins
	}
	user.SetWantedMins(r, s.db, wantedMins)

	user.Code = GenCode(r, s.db)
	UUIDKey, userExists := user.GetUUIDKey(r, s.db)

	if userExists && len(UUIDKey) > 0 && !user.IsValid(r, s.db) {
		WriteError(w, r, 402, "Invalid UUID key. Ask hello@transferme.it to reset")
		return
	} else if !userExists {
		// create new tmi user
		Log(r, log.InfoLevel, fmt.Sprintf("Creating new user %s", user.UUID))
		user.UUIDKey = RandomString(keyUUIDLen)
		user.MaxFileSize = freeFileUploadBytes
		user.BandwidthLeft = freeBandwidthBytes
		user.MinsAllowed = defaultAccountLifeMins
		user.WantedMins = defaultAccountLifeMins
		user.Expiry = time.Now().Add(time.Minute * time.Duration(defaultAccountLifeMins)).UTC()
		if err := user.Store(r, s.db); err != nil {
			WriteError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		if len(UUIDKey) == 0 {
			// if key has been removed from db because of lost UUID key from client
			Log(r, log.InfoLevel, fmt.Sprintf("Resetting UUID key for: %s", user.UUID))

			user.UUIDKey = RandomString(keyUUIDLen)
			if err := user.UpdateUUIDKey(r, s.db); err != nil {
				WriteError(w, r, http.StatusInternalServerError, err.Error())
				return
			}
		} else {
			user.UUIDKey = ""
		}

		// set perm code at client request
		expectedPermCode := r.Form.Get("perm_user_code")
		if len(expectedPermCode) != 0 {
			if err := SetUsersPermCode(r, s.db, &user, expectedPermCode); err != nil {
				WriteError(w, r, http.StatusInternalServerError, err.Error())
				return
			}
		}

		user.Expiry = time.Now().Add(time.Minute * time.Duration(user.WantedMins)).UTC()
		if err := user.Update(r, s.db); err != nil {
			WriteError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
	}

	_ = WriteJSON(w, user)
}

// InitUploadHandler tells the server what to suspect in the UploadHandler and handles most file upload validation.
// This is done before so as to not have to wait for the file to be transferred to the server.
func (s *Server) InitUploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, r, http.StatusBadRequest, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		WriteError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !user.IsValid(r, s.db) {
		WriteError(w, r, http.StatusBadRequest, "Invalid method")
		return
	}

	filesize, err := strconv.ParseInt(r.Form.Get("filesize"), 10, 64)
	if err != nil {
		WriteError(w, r, InvalidFileSizeErrorCode, "Invalid value for filesize") // TODO test
		return
	}

	code := r.Form.Get("code")
	friend, err := CodeToUser(r, s.db, code)
	if err != nil {
		WriteError(w, r, InvalidFriendErrorCode, err.Error())
		return
	}
	if friend.UUID == "" || friend.PublicKey == "" {
		WriteError(w, r, InvalidFriendErrorCode, "Your friend does not exist!")
		return
	}

	if friend.UUID == Hash(user.UUID) {
		WriteError(w, r, SendToSelfErrorCode, "Your can't send files to yourself!")
		return
	}

	if err := user.GetWantedMins(r, s.db); err != nil {
		WriteError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	user.GetBandwidthLeft(r, s.db)
	if user.BandwidthLeft-filesize < 0 {
		WriteError(w, r, BandwidthLimitErrorCode, "This transfer exceeds today's bandwidth limit!")
		return
	}

	user.GetMaxFileSize(r, s.db)
	if user.MaxFileSize-filesize < 0 {
		Log(r, log.InfoLevel, fmt.Sprintf("transfer with %f difference", BytesToMegabytes(user.MaxFileSize-filesize)))
		mb := BytesToMegabytes(user.MaxFileSize)
		m := fmt.Sprintf("This transfer exceeds your %fMB max file transfer Size!", mb)
		WriteError(w, r, FileSizeLimitErrorCode, m)
		return
	}

	transfer := Transfer{
		From: user,
		To:   User{UUID: friend.UUID},
		Size: filesize,
	}

	if transfer.AlreadyToUser(r, s.db) {
		// already uploading to friend so delete the currently in process transfer
		if err := transfer.Completed(r, s, true, false); err != nil {
			WriteError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
	}

	transfer.ID, err = transfer.InitialStore(r, s.db)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	transfer.From.UUID = "" // for privacy remove the UUID

	// store transfer information in SESSION to be picked up by UploadHandler
	if err := StoreSession(r, w, s.session, uploadSessionName, transfer); err != nil {
		WriteError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	_, _ = w.Write([]byte(friend.PublicKey))
}

// UploadHandler handles the file upload but only works after running InitUploadHandler
func (s *Server) UploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, r, http.StatusBadRequest, "Invalid method")
		return
	}

	var sessionTransfer Transfer
	if err := GetSession(r, s.session, uploadSessionName, &sessionTransfer); err != nil {
		WriteError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	if sessionTransfer.To.UUID == "" {
		WriteError(w, r, http.StatusBadRequest, "No upload cookie")
		return
	}

	if err := r.ParseMultipartForm(int64(maxFileUploadSizeMB << 20)); err != nil {
		WriteError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// get POST data
	if err := r.ParseForm(); err != nil {
		WriteError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// get (encrypted with friends public key) password
	sessionTransfer.Password = r.Form.Get("password")

	// get file from form
	file, handler, err := r.FormFile("file")
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			Log(r, log.WarnLevel, err.Error())
		}
	}()

	// verify session data from InitUploadHandler against actual file Size
	// should be less than expected as it should have been compressed since.
	if handler.Size > sessionTransfer.Size {
		m := fmt.Sprintf("You lied about the transfer size expected %v got %v!", sessionTransfer.Size, int(handler.Size))
		WriteError(w, r, http.StatusBadRequest, m)
		return
	}

	// write file to minio
	objectName := RandomString(userDirLen) + "/" + handler.Filename
	_, err = tminio.PutObject(r, s.minio, context.Background(), bucketName, objectName, file, handler.Size,
		minio.PutObjectOptions{
			ContentType: fileContentType,
		})
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	// write details into the transfer struct
	transfer := sessionTransfer
	transfer.ObjectName = objectName
	transfer.expiry = time.Now().Add(time.Minute * time.Duration(sessionTransfer.From.WantedMins))
	transfer.Size = handler.Size

	if err := transfer.Store(r, s.db); err != nil {
		WriteError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	// tell friend to download file
	if err := s.funnels.Send(s.redis, Hash(Hash(transfer.To.UUID)), SocketMessage{ObjectPath: objectName}); err != nil {
		// no websocket to forward message on to so store until reconnect
		Log(r, log.InfoLevel, fmt.Sprintf("%s is not online", transfer.To.UUID))
	}
}

// DownloadHandler handles the download of the file
func (s *Server) DownloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, r, http.StatusBadRequest, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		WriteError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	// get encrypted (with friends public key) password
	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !user.IsValid(r, s.db) {
		WriteError(w, r, http.StatusBadRequest, "Invalid form data")
		return
	}

	objectName := r.Form.Get("object_name")
	if !AllowedToDownload(r, s.db, user, objectName) {
		WriteError(w, r, http.StatusBadRequest, "No such file at path!")
		return
	}

	obj, err := tminio.GetObject(r, s.minio, context.Background(), bucketName, objectName, minio.GetObjectOptions{})
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	fi, err := obj.Stat()
	if err != nil {
		WriteError(w, r, http.StatusBadRequest, err.Error())
		return
	} else if fi.Size == 0 {
		WriteError(w, r, http.StatusBadRequest, "File has been deleted from server")
		return
	}

	w.Header().Set("Content-Type", fileContentType)
	w.Header().Set("Content-Transfer-Encoding", "binary")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fi.Size))

	_, err = io.Copy(w, obj)
	if err != nil {
		WriteError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
}

// CompletedDownloadHandler fetches the encrypted password of the uploaded file if passed a valid file hash
func (s *Server) CompletedDownloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, r, http.StatusBadRequest, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		WriteError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	var user = User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !user.IsValid(r, s.db) {
		WriteError(w, r, http.StatusBadRequest, "Invalid user")
		return
	}

	var transfer = Transfer{
		To:         User{UUID: user.UUID},
		ObjectName: r.Form.Get("object_name"),
	}

	failedTransfer := false
	if err := transfer.GetPasswordAndUUID(r, s.db); err != nil {
		failedTransfer = true
		WriteError(w, r, http.StatusInternalServerError, err.Error())
	} else {
		if transfer.Password == "" || transfer.From.UUID == "" {
			failedTransfer = true
			WriteError(w, r, http.StatusInternalServerError, "No password for user")
		}
	}

	if err := transfer.Completed(r, s, failedTransfer, false); err != nil {
		WriteError(w, r, http.StatusInternalServerError, err.Error())
		return
	}

	_, err := w.Write([]byte(transfer.Password))
	Log(r, log.ErrorLevel, err)
}
