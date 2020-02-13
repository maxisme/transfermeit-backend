package main

import (
	"encoding/gob"
	"encoding/json"
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
	WSClients      = make(map[string]*websocket.Conn)
	WSClientsMutex = sync.RWMutex{}

	Upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

const UploadSessionName = "upload"

func SecKeyHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Sec-Key") != os.Getenv("server_key") {
			writeError(w, r, 400, "Invalid form data")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) TogglePermCodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, r, 400, "Invalid method")
		return
	}

	// fetch post data
	if err := r.ParseForm(); err != nil {
		writeError(w, r, 400, "Invalid form data")
		return
	}

	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !IsValidUserCredentials(s.db, user) {
		log.Println("Invalid credentials")
		writeError(w, r, 400, "Invalid form data")
		return
	}

	SetUserTier(s.db, &user)
	if user.Tier >= PermUserTier {
		permCode, customCode := GetUserPermCode(s.db, user)
		if permCode.Valid || customCode.Valid {
			// remove any stored codes (INCLUDING custom code)
			err := RemovePermCodes(s.db, user)
			Handle(err)
		} else {
			// turn on random perm code
			permCode := GenUserCode(s.db)
			if err := SetPermCode(s.db, permCode, user); err != nil {
				writeError(w, r, 401, "Failed to set permanent code")
				return
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	jsonReply, _ := json.Marshal(user)
	_, _ = w.Write(jsonReply)
}

func (s *Server) CustomCodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, r, 400, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		writeError(w, r, 400, "Invalid form data")
		return
	}

	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !IsValidUserCredentials(s.db, user) {
		writeError(w, r, 400, "Invalid form data")
		return
	}

	customCode := r.Form.Get("custom_code")
	if len(customCode) != CodeLen {
		writeError(w, r, 401, "Invalid custom code")
		return
	}

	SetUserTier(s.db, &user)
	if user.Tier >= CustomCodeUserTier {
		if err := SetCustomCode(s.db, customCode, user); err == nil {
			jsonReply, err := json.Marshal(user)
			Handle(err)
			w.Header().Set("Content-Type", "application/json")
			_, err = w.Write(jsonReply)
			Handle(err)
			return
		} else {
			Handle(err)
		}
	}
	writeError(w, r, 402, "Failed to set activation code")
}

func (s *Server) RegisterCreditHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, r, 400, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		writeError(w, r, 400, "Invalid form data")
		return
	}

	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !IsValidUserCredentials(s.db, user) {
		writeError(w, r, 400, "Invalid form data")
		return
	}

	creditCode := r.Form.Get("credit_code")
	if len(creditCode) == CreditCodeLen {
		if err := SetCreditCode(s.db, user, creditCode); err != nil {
			writeError(w, r, 401, "Failed to register credit")
			return
		}
	}
}

func (s *Server) CredentialHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, r, 400, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		writeError(w, r, 400, "Invalid form data")
		return
	}

	user := User{}
	user.UUID = r.Form.Get("UUID")
	user.UUIDKey = r.Form.Get("UUID_key")
	user.PublicKey = r.Form.Get("public_key")

	if user.UUID == "" {
		writeError(w, r, 400, "Invalid form data")
		return
	}

	if user.PublicKey == "" {
		writeError(w, r, 401, "Invalid form data")
		return
	}

	if !IsValidPublicKey(user.PublicKey) {
		writeError(w, r, 401, "Invalid public key in keychain!")
		return
	}

	wantedMins, err := strconv.Atoi(r.Form.Get("wanted_mins")) // convert to int
	if err != nil || wantedMins <= 0 || wantedMins%5 != 0 || wantedMins > MaxAccountLifeMins {
		wantedMins = DefaultAccountLifeMins
	}

	user.Code = GenUserCode(s.db)
	UUIDKey, userExists := GetUUIDKey(s.db, user)

	if userExists && len(UUIDKey) > 0 && !IsValidUserCredentials(s.db, user) {
		writeError(w, r, 402, "Invalid UUID key. Ask hello@transferme.it to reset")
		return
	} else if !userExists {
		log.Println("Creating new user " + user.UUID)
		// create new tmi user
		user.UUIDKey = RandomString(UUIDKeyLen)
		user.MaxFileSize = FreeFileUploadBytes
		user.Bandwidth = FreeBandwidthBytes
		user.MinsAllowed = DefaultAccountLifeMins
		user.WantedMins = DefaultAccountLifeMins
		user.Expiry = time.Now().Add(time.Minute * time.Duration(wantedMins)).UTC()
		go CreateNewUser(s.db, user)
	} else {
		if len(UUIDKey) == 0 {
			// if key has been removed from db because of lost UUID key from client
			log.Println("Resetting UUID key for " + user.UUID)

			user.UUIDKey = RandomString(UUIDKeyLen)
			go SetUserUUIDKey(s.db, user)
		} else {
			user.UUIDKey = ""
		}

		// set perm code at client request
		expectedPermCode := r.Form.Get("perm_user_code")
		if len(expectedPermCode) != 0 {
			SetUsersPermCode(s.db, &user, expectedPermCode)
		}

		SetUserStats(s.db, &user)

		if wantedMins > user.MinsAllowed {
			wantedMins = DefaultAccountLifeMins
		}
		user.WantedMins = wantedMins

		user.Expiry = time.Now().Add(time.Minute * time.Duration(wantedMins)).UTC()
		go UpdateUser(s.db, user)
	}

	// return json of user
	jsonReply, err := json.Marshal(user)
	Handle(err)
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(jsonReply)
	Handle(err)
}

// the InitUploadHandler tells the server what to suspect in the UploadHandler and
// handles most validation such as file and bandwidth limits for the user.
// This is done before so as to not have to wait for the to be transfered to the server
func (s *Server) InitUploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, r, 400, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		writeError(w, r, 400, "Invalid form data")
		return
	}

	filesize, err := strconv.Atoi(r.Form.Get("filesize"))
	if err != nil {
		writeError(w, r, 401, "Invalid value for filesize")
		return
	}

	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !IsValidUserCredentials(s.db, user) {
		writeError(w, r, 400, "Method not allowed")
		return
	}

	code := r.Form.Get("code")
	friend := CodeToUser(s.db, code)
	if friend.UUID == "" || friend.PublicKey == "" {
		writeError(w, r, 401, "Your friend does not exist!")
		return
	}

	if friend.UUID == Hash(user.UUID) {
		writeError(w, r, 401, "Your can't send files to yourself!")
		return
	}

	SetUserWantedMins(s.db, &user)

	SetUserMaxFileUpload(s.db, &user)
	if user.MaxFileSize-filesize < 0 {
		log.Printf("transfer with %v difference", BytesToMegabytes(user.MaxFileSize-filesize))
		mb := BytesToMegabytes(user.MaxFileSize)
		m := fmt.Sprintf("This transfer exceeds your %fMB max file transfer Size!", mb)
		writeError(w, r, 401, m)
		return
	}
	userBandwidthLeft := GetUserBandwidthLeft(s.db, &user)
	if userBandwidthLeft-filesize < 0 {
		writeError(w, r, 401, "This transfer exceeds today's bandwidth limit!")
		return
	}

	transfer := Transfer{
		from: user,
		to:   User{UUID: friend.UUID},
		Size: filesize,
	}

	if IsAlreadyTransferring(s.db, &transfer) {
		// already uploading to friend so delete the currently in process transfer
		log.Println("Already uploading file from " + transfer.from.UUID + " to " + transfer.to.UUID)
		go CompleteTransfer(s.db, transfer, true, false)
	}

	transfer.ID = InsertTransfer(s.db, transfer)
	transfer.from.UUID = "" // for privacy remove the UUID

	// store transfer information in session to be picked up by UploadHandler
	session := InitSession(r)
	gob.Register(Transfer{})
	session.Values[UploadSessionName] = transfer
	err = session.Save(r, w)
	Handle(err)

	_, err = w.Write([]byte(friend.PublicKey))
	Handle(err)
}

// Only works after running InitUploadHandler
func (s *Server) UploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, r, 400, "Invalid method")
		return
	}

	// get session transfer
	session := InitSession(r)
	sessionTransfer := session.Values[UploadSessionName].(Transfer)
	if sessionTransfer.ID == 0 {
		writeError(w, r, 401, "Init transfer not run")
		return
	}

	// delete session
	session.Values[UploadSessionName] = nil
	err := session.Save(r, w)
	Handle(err)

	err = r.ParseMultipartForm(int64(MaxFileUploadSizeMB << 20))
	Handle(err)

	// get POST data
	if err := r.ParseForm(); err != nil {
		Handle(err)
		writeError(w, r, 400, "Invalid form data")
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
		writeError(w, r, 401, m)
		return
	}

	// extract file to bytes
	fileBytes, err := ioutil.ReadAll(file)
	Handle(err)

	// write file to server
	dir := FILEDIR + RandomString(UserDirLen)
	err = os.MkdirAll(dir, 0744)
	Handle(err)
	fileLocation := dir + "/" + handler.Filename
	err = ioutil.WriteFile(fileLocation, fileBytes, 0744)
	Handle(err)

	// write full details in transfer struct
	transfer := sessionTransfer
	transfer.FilePath = strings.Replace(fileLocation, FILEDIR, "", -1)
	transfer.expiry = time.Now().Add(time.Minute * time.Duration(sessionTransfer.from.WantedMins))
	transfer.hash = HashBytes(fileBytes)
	transfer.Size = int(handler.Size)

	Handle(UpdateTransfer(s.db, transfer))

	// tell friend to download file
	SendSocketMessage(SocketMessage{
		Download: &transfer,
	}, transfer.to.UUID, true)
}

func (s *Server) DownloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, r, 400, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		Handle(err)
		writeError(w, r, 400, "Invalid form data")
		return
	}

	// get encrypted (with friends public key) password
	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !IsValidUserCredentials(s.db, user) {
		log.Println("Invalid credentials")
		writeError(w, r, 400, "Invalid form data")
		return
	}

	filePath := r.Form.Get("file_path")
	if !AllowedToDownloadPath(s.db, user, filePath) {
		writeError(w, r, 401, "No such file at path!")
		return
	}

	f, err := os.Open(FILEDIR + filePath)
	if err != nil {
		Handle(err)
		writeError(w, r, 401, err.Error())
		return
	}
	fi, err := f.Stat()
	if err != nil {
		Handle(err)
		writeError(w, r, 401, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Transfer-Encoding", "binary")
	w.Header().Set("Content-Length", string(fi.Size()))

	_, err = io.Copy(w, f)
	Handle(err)
}

func (s *Server) CompletedDownloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		writeError(w, r, 400, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		Handle(err)
		writeError(w, r, 400, "Invalid form data")
		return
	}

	var user = User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !IsValidUserCredentials(s.db, user) {
		writeError(w, r, 400, "Invalid form data")
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
		password, fromUUID := GetTransferPasswordAndUUID(s.db, transfer)
		if password == "" || fromUUID == "" {
			log.Println("No password for user. Or already uploading to user", transfer)
			writeError(w, r, 402, "No password for user")
		} else {
			transfer.from.UUID = fromUUID
		}
		_, err := w.Write([]byte(password))
		Handle(err)
	}

	CompleteTransfer(s.db, transfer, failed, false)
}
