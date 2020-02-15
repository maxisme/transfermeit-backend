package main

import (
	"encoding/gob"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	clientsWS      = make(map[string]*websocket.Conn)
	clientsWSMutex = sync.RWMutex{}

	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

const uploadSessionName = "upload"

// TogglePermCodeHandler either turns on or off a users perm code depending if they have one already
func (s *Server) TogglePermCodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, r, 400, "Invalid method")
		return
	}

	// fetch post data
	if err := r.ParseForm(); err != nil {
		WriteError(w, r, 400, "Invalid form data")
		return
	}

	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !user.IsValid(s.db) {
		log.Println("Invalid credentials")
		WriteError(w, r, 400, "Invalid form data")
		return
	}

	user.GetTier(s.db)
	if user.Tier >= permUserTier {
		permCode, customCode := GetUserPermCode(s.db, user)
		if permCode.Valid || customCode.Valid {
			// remove any stored codes (INCLUDING custom code) as they already have one or the other
			err := RemovePermCodes(s.db, user)
			Handle(err)
		} else {
			// turn on random perm code
			user.Code = GenCode(s.db)
			if err := SetPermCode(s.db, user); err != nil {
				WriteError(w, r, 401, "Failed to set permanent code")
				return
			}
		}
	}

	Handle(WriteJSON(w, user))
}

// CustomCodeHandler sets a users custom code
func (s *Server) CustomCodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, r, 400, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		WriteError(w, r, 400, "Invalid form data")
		return
	}

	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !user.IsValid(s.db) {
		WriteError(w, r, 400, "Invalid form data")
		return
	}

	user.Code = r.Form.Get("custom_code")
	if len(user.Code) != codeLen {
		WriteError(w, r, 401, "Invalid custom code")
		return
	}

	user.GetTier(s.db)
	if user.Tier >= customCodeUserTier {
		err := SetCustomCode(s.db, user)
		if err == nil {
			Handle(WriteJSON(w, user))
			return
		}
		Handle(err)
	}
	WriteError(w, r, 402, "Failed to set activation code")
}

// RegisterCreditHandler will associate a credit code to an account
func (s *Server) RegisterCreditHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, r, 400, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		WriteError(w, r, 400, "Invalid form data")
		return
	}

	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !user.IsValid(s.db) {
		WriteError(w, r, 400, "Invalid form data")
		return
	}

	creditCode := r.Form.Get("credit_code")
	if len(creditCode) == CreditCodeLen {
		if err := SetCreditCode(s.db, user, creditCode); err != nil {
			WriteError(w, r, 401, "Failed to register credit")
			return
		}
	}
}

// CreateCodeHandler creates an account and/or updates a users code
func (s *Server) CreateCodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, r, 400, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		WriteError(w, r, 400, "Invalid form data")
		return
	}

	user := User{}
	user.UUID = r.Form.Get("UUID")
	user.UUIDKey = r.Form.Get("UUID_key")
	user.PublicKey = r.Form.Get("public_key")

	if user.UUID == "" {
		WriteError(w, r, 400, "Invalid form data")
		return
	}

	if user.PublicKey == "" {
		WriteError(w, r, 401, "Invalid form data")
		return
	}

	if !IsValidPublicKey(user.PublicKey) {
		WriteError(w, r, 401, "Invalid public key in keychain!")
		return
	}

	wm, err := strconv.Atoi(r.Form.Get("wanted_mins")) // convert to int
	if err != nil {
		wm = defaultAccountLifeMins
	}
	user.SetWantedMins(s.db, wm)

	user.Code = GenCode(s.db)
	UUIDKey, userExists := user.GetUUIDKey(s.db)

	if userExists && len(UUIDKey) > 0 && !user.IsValid(s.db) {
		WriteError(w, r, 402, "Invalid UUID key. Ask hello@transferme.it to reset")
		return
	} else if !userExists {
		// create new tmi user
		log.Println("Creating new user " + user.UUID)
		user.UUIDKey = RandomString(keyUUIDLen)
		user.MaxFileSize = freeFileUploadBytes
		user.BandwidthLeft = freeBandwidthBytes
		user.MinsAllowed = defaultAccountLifeMins
		user.WantedMins = defaultAccountLifeMins
		user.Expiry = time.Now().Add(time.Minute * time.Duration(defaultAccountLifeMins)).UTC()
		go user.Store(s.db)
	} else {
		if len(UUIDKey) == 0 {
			// if key has been removed from db because of lost UUID key from client
			log.Println("Resetting UUID key for " + user.UUID)

			user.UUIDKey = RandomString(keyUUIDLen)
			go user.UpdateUUIDKey(s.db)
		} else {
			user.UUIDKey = ""
		}

		// set perm code at client request
		expectedPermCode := r.Form.Get("perm_user_code")
		if len(expectedPermCode) != 0 {
			SetUsersPermCode(s.db, &user, expectedPermCode)
		}

		user.Expiry = time.Now().Add(time.Minute * time.Duration(user.WantedMins)).UTC()
		go user.Update(s.db)
	}

	Handle(WriteJSON(w, user))
}

// InitUploadHandler tells the server what to suspect in the UploadHandler and handles most file upload validation.
// This is done before so as to not have to wait for the file to be transferred to the server.
func (s *Server) InitUploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, r, 400, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		WriteError(w, r, 400, "Invalid form data")
		return
	}

	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !user.IsValid(s.db) {
		WriteError(w, r, 400, "Invalid method")
		return
	}

	filesize, err := strconv.Atoi(r.Form.Get("filesize"))
	if err != nil {
		WriteError(w, r, 401, "Invalid value for filesize") // TODO test
		return
	}

	code := r.Form.Get("code")
	friend := CodeToUser(s.db, code)
	if friend.UUID == "" || friend.PublicKey == "" {
		WriteError(w, r, 402, "Your friend does not exist!")
		return
	}

	if friend.UUID == Hash(user.UUID) {
		WriteError(w, r, 403, "Your can't send files to yourself!")
		return
	}

	user.GetWantedMins(s.db)

	user.GetBandwidthLeft(s.db)
	if user.BandwidthLeft-filesize < 0 {
		WriteError(w, r, 404, "This transfer exceeds today's bandwidth limit!")
		return
	}

	user.GetMaxFileSize(s.db)
	if user.MaxFileSize-filesize < 0 {
		log.Printf("transfer with %v difference", BytesToMegabytes(user.MaxFileSize-filesize))
		mb := BytesToMegabytes(user.MaxFileSize)
		m := fmt.Sprintf("This transfer exceeds your %fMB max file transfer Size!", mb)
		WriteError(w, r, 405, m)
		return
	}

	transfer := Transfer{
		from: user,
		to:   User{UUID: friend.UUID},
		Size: filesize,
	}

	transfer.GetLiveFilePath(s.db)
	if len(transfer.FilePath) > 0 {
		// already uploading to friend so delete the currently in process transfer
		log.Print("jnkafsdfjsdnklfndskfadsfla")
		go transfer.Completed(s.db, true, false)
	}

	transfer.ID = transfer.InitialStore(s.db)
	transfer.from.UUID = "" // for privacy remove the UUID

	// store transfer information in session to be picked up by UploadHandler
	session := InitSession(r)
	gob.Register(Transfer{})
	session.Values[uploadSessionName] = transfer
	err = session.Save(r, w)
	Handle(err)

	_, err = w.Write([]byte(friend.PublicKey))
	Handle(err)
}

// UploadHandler handles the file upload but only works after running InitUploadHandler
func (s *Server) UploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, r, 400, "Invalid method")
		return
	}

	// get session transfer
	session := InitSession(r)
	sessionTransfer := session.Values[uploadSessionName].(Transfer)
	if sessionTransfer.ID == 0 {
		WriteError(w, r, 401, "Init transfer not run")
		return
	}

	// delete session
	session.Values[uploadSessionName] = nil
	err := session.Save(r, w)
	Handle(err)

	err = r.ParseMultipartForm(int64(maxFileUploadSizeMB << 20))
	Handle(err)

	// get POST data
	if err := r.ParseForm(); err != nil {
		Handle(err)
		WriteError(w, r, 400, "Invalid form data")
		return
	}

	// get (encrypted with friends public key) password
	sessionTransfer.password = r.Form.Get("password")

	// get file from form
	file, handler, err := r.FormFile("file")
	Handle(err)
	defer file.Close()

	// verify session data from InitUploadHandler against actual file Size
	// should be less than expected as it should have been compressed since.
	if int(handler.Size) > sessionTransfer.Size {
		m := fmt.Sprintf("You lied about the transfer size expected %v got %v!", sessionTransfer.Size, int(handler.Size))
		WriteError(w, r, 401, m)
		return
	}

	// extract file to bytes
	fileBytes, err := ioutil.ReadAll(file)
	Handle(err)

	// write file to server
	dir := fileStoreDirectory + RandomString(userDirLen)
	err = os.MkdirAll(dir, 0744)
	Handle(err)
	fileLocation := dir + "/" + handler.Filename
	err = ioutil.WriteFile(fileLocation, fileBytes, 0744)
	Handle(err)

	// write full details in transfer struct
	transfer := sessionTransfer
	transfer.FilePath = strings.Replace(fileLocation, fileStoreDirectory, "", -1)
	transfer.expiry = time.Now().Add(time.Minute * time.Duration(sessionTransfer.from.WantedMins))
	transfer.hash = HashWithBytes(fileBytes)
	transfer.Size = int(handler.Size)

	Handle(transfer.Store(s.db))

	// tell friend to download file
	SendSocketMessage(SocketMessage{
		Download: &transfer,
	}, transfer.to.UUID, true)
}

// DownloadHandler handles the download of the file
func (s *Server) DownloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, r, 400, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		Handle(err)
		WriteError(w, r, 400, "Invalid form data")
		return
	}

	// get encrypted (with friends public key) password
	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !user.IsValid(s.db) {
		WriteError(w, r, 400, "Invalid form data")
		return
	}

	filePath := r.Form.Get("file_path")
	if !AllowedToDownload(s.db, user, filePath) {
		WriteError(w, r, 401, "No such file at path!")
		return
	}

	f, err := os.Open(fileStoreDirectory + filePath)
	if err != nil {
		Handle(err)
		WriteError(w, r, 401, err.Error())
		return
	}
	fi, err := f.Stat()
	if err != nil {
		Handle(err)
		WriteError(w, r, 401, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Transfer-Encoding", "binary")
	w.Header().Set("Content-Length", string(fi.Size()))

	_, err = io.Copy(w, f)
	Handle(err)
}

// CompletedDownloadHandler fetches the encrypted password of the uploaded file if passed a valid file hash
func (s *Server) CompletedDownloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, r, 400, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		Handle(err)
		WriteError(w, r, 400, "Invalid form data")
		return
	}

	var user = User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !user.IsValid(s.db) {
		WriteError(w, r, 400, "Invalid form data")
		return
	}

	var transfer = Transfer{
		to:       User{UUID: user.UUID},
		FilePath: r.Form.Get("file_path"),
		hash:     r.Form.Get("hash"),
	}

	failed := true
	if transfer.hash != "" {
		failed = false
		transfer.GetPasswordAndUUID(s.db)
		if transfer.password == "" || transfer.from.UUID == "" {
			log.Println("No password for user. Or already uploading to user", transfer)
			WriteError(w, r, 402, "No password for user")
		}
		_, err := w.Write([]byte(transfer.password))
		Handle(err)
	}

	transfer.Completed(s.db, failed, false)
}
