package main

import (
	"bytes"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
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
	clients      = make(map[string]*websocket.Conn)
	clientsMutex = sync.RWMutex{}
)
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func SecKeyHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Sec-Key") != os.Getenv("server_key") {
			http.Error(w, "Invalid form data", 400)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) TogglePermCodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, 400, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		WriteError(w, 400, "Invalid form data")
		return
	}

	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !IsValidUserCredentials(s.db, user) {
		log.Println("Invalid credentials")
		http.Error(w, "Method not allowed", 400)
		return
	}

	SetUsersTier(s.db, &user)
	if user.Tier >= PERMUSER {
		permCode, customCode := GetUsersPermCode(s.db, user)
		if permCode.Valid || customCode.Valid {
			// toggle off any stored codes (INCLUDING custom code)
			err := RemovePermCodes(s.db, user)
			Handle(err)
		} else {
			// turn on random perm code
			user.Code = GenUserCode(s.db)
			if err := SetPermCode(s.db, user); err != nil {
				Handle(err)
				WriteError(w, 401, "Failed to set permanent code")
				return
			}
		}
	}
	jsonReply, _ := json.Marshal(user)
	_, _ = w.Write(jsonReply)
}

func (s *Server) CustomCodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, 400, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		WriteError(w, 400, "Invalid form data")
		return
	}

	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !IsValidUserCredentials(s.db, user) {
		log.Println("Invalid credentials")
		http.Error(w, "Method not allowed", 400)
		return
	}

	user.Code = r.Form.Get("custom_code")
	if len(user.Code) != CODELEN {
		http.Error(w, "Invalid custom code", 401)
		return
	}

	SetUsersTier(s.db, &user)
	if user.Tier >= CODEUSER {
		if err := SetCustomCode(s.db, user); err == nil {
			jsonReply, _ := json.Marshal(user)
			_, _ = w.Write(jsonReply)
			return
		} else {
			Handle(err)
		}
	}
	WriteError(w, 402, "Failed to set activation code")
}

func (s *Server) RegisterCreditHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, 400, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		WriteError(w, 400, "Invalid form data")
		return
	}

	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !IsValidUserCredentials(s.db, user) {
		log.Println("Invalid credentials")
		http.Error(w, "Method not allowed", 400)
		return
	}

	creditCode := r.Form.Get("credit_code")
	if len(creditCode) == CREDITCODELEN {
		if err := SetCreditCode(s.db, user, creditCode); err != nil {
			Handle(err)
			WriteError(w, 401, "Failed to register credit")
			return
		}
	}
}

func (s *Server) CredentialHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, 400, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		WriteError(w, 400, "Invalid form data")
		return
	}

	if r.Form.Get("UUID") == "" {
		WriteError(w, 400, "Invalid form data")
		return
	}

	user := User{}
	user.UUID = r.Form.Get("UUID")
	user.UUIDKey = r.Form.Get("UUID_key")
	user.PublicKey = r.Form.Get("public_key")

	if user.PublicKey == "" {
		WriteError(w, 401, "Invalid form data")
		return
	}

	if !IsValidPublicKey(user.PublicKey) {
		WriteError(w, 401, "Invalid public key in keychain!")
		return
	}

	wantedMins, err := strconv.Atoi(r.Form.Get("wanted_mins")) // convert to int
	if err != nil || wantedMins <= 0 || wantedMins%5 != 0 || wantedMins > MAXMINS {
		wantedMins = DEFAULTMIN
	}

	user.Code = GenUserCode(s.db)

	if !IsValidUserCredentials(s.db, user) {
		// creating new user account
		if HasUUID(s.db, user) {
			WriteError(w, 402, "Invalid UUID key")
			return
		} else {
			log.Println("Creating new user " + user.UUID)
			// create new tmi user
			user.UUIDKey = RandomString(UUIDKEYLEN)
			user.MaxFileSize = FREEFILEUPLOAD
			user.Bandwidth = FREEBANDWIDTH
			user.MinsAllowed = DEFAULTMIN
			user.WantedMins = DEFAULTMIN
			user.EndTime = time.Now().Add(time.Minute * time.Duration(wantedMins)).UTC()
			go CreateNewUser(s.db, user)
		}
	} else {
		// creating new code for user
		expectedPermCode := r.Form.Get("perm_user_code")
		if expectedPermCode != "" {
			SetUsersPermCode(s.db, &user, r.Form.Get("perm_user_code"))
		}
		SetUserStats(s.db, &user)

		if wantedMins > user.MinsAllowed {
			wantedMins = DEFAULTMIN
		}
		user.WantedMins = wantedMins

		user.EndTime = time.Now().Add(time.Minute * time.Duration(wantedMins)).UTC()
		user.UUIDKey = ""
		go UpdateUser(s.db, user)
	}

	jsonReply, err := json.Marshal(user)
	Handle(err)
	_, err = w.Write(jsonReply)
	Handle(err)
}

func (s *Server) InitUploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		WriteError(w, 400, "Invalid method")
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		WriteError(w, 400, "Invalid form data")
		return
	}

	filesize, err := strconv.Atoi(r.Form.Get("filesize"))
	if err != nil {
		Handle(err)
		WriteError(w, 401, "Invalid value for filesize")
		return
	}

	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !IsValidUserCredentials(s.db, user) {
		log.Println("Invalid credentials")
		WriteError(w, 400, "Method not allowed")
		return
	}

	code := r.Form.Get("code")
	friend := CodeToUser(s.db, code)
	if friend.UUID == "" || friend.PublicKey == "" {
		WriteError(w, 401, "Your friend does not exist!")
		return
	}

	if friend.UUID == Hash(user.UUID) {
		WriteError(w, 401, "Your can't send files to yourself!")
		return
	}

	SetUserWantedMins(s.db, &user)

	SetUsersMaxFileUpload(s.db, &user)
	if user.MaxFileSize-filesize < 0 {
		log.Printf("transfer with %v difference", BytesToMegabytes(user.MaxFileSize-filesize))
		mb := BytesToMegabytes(user.MaxFileSize)
		m := fmt.Sprintf("This transfer exceeds your %fMB max file transfer Size!", mb)
		WriteError(w, 401, m)
		return
	}
	userBandwidthLeft := GetUserBandwidthLeft(s.db, &user)
	if userBandwidthLeft-filesize < 0 {
		WriteError(w, 401, "This transfer exceeds today's bandwidth limit!")
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
	transfer.from.UUID = "" // for privacy reasons

	// store transfer in session
	session := InitSession(r)
	gob.Register(Transfer{})
	session.Values[UPLOADSESSIONNAME] = transfer
	err = session.Save(r, w)
	Handle(err)

	_, err = w.Write([]byte(friend.PublicKey))
	Handle(err)
}

func (s *Server) UploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 400)
		return
	}

	err := r.ParseMultipartForm(int64(GLOBALMAXFILESIZEMB << 20))
	Handle(err)

	// fetch form
	if err := r.ParseForm(); err != nil {
		WriteError(w, 400, "Invalid form data")
		return
	}

	// get session transfer
	session := InitSession(r)
	if session.Values[UPLOADSESSIONNAME] == nil {
		WriteError(w, 401, "Init transfer not run")
		return
	}
	transfer := session.Values[UPLOADSESSIONNAME].(Transfer)
	if transfer.ID == 0 {
		WriteError(w, 401, "Init transfer not run")
		return
	}
	session.Values[UPLOADSESSIONNAME] = nil
	err = session.Save(r, w)
	Handle(err)

	// get encrypted with friends public key password
	transfer.password = r.Form.Get("password")

	// get file from form
	file, handler, err := r.FormFile("file")
	defer file.Close()
	Handle(err)

	// verify init transfer against actual file Size
	// should be less than expected as it is compressed
	if int(handler.Size) > transfer.Size {
		m := fmt.Sprintf("You lied about the transfer Size expected %v got %v!", transfer.Size, int(handler.Size))
		WriteError(w, 401, m)
		return
	}
	transfer.Size = int(handler.Size)

	// get file bytes
	fileBytes, err := ioutil.ReadAll(file)
	Handle(err)

	// get file hash
	transfer.hash = FileHash(fileBytes)

	// write file
	dir := FILEDIR + RandomString(USERDIRLEN)
	err = os.MkdirAll(dir, 0744)
	Handle(err)
	fileLocation := dir + "/" + handler.Filename
	err = ioutil.WriteFile(fileLocation, fileBytes, 0744)
	Handle(err)

	transfer.FilePath = strings.Replace(fileLocation, FILEDIR, "", -1)
	transfer.expiry = time.Now().Add(time.Minute * time.Duration(transfer.from.WantedMins))

	err = UpdateTransfer(s.db, transfer)
	Handle(err)

	SendSocketMessage(SocketMessage{
		Download: &transfer,
	}, transfer.to.UUID, true)
}

func (s *Server) DownloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 400)
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		WriteError(w, 400, "Invalid form data")
		return
	}

	// get encrypted (with friends public key) password
	user := User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !IsValidUserCredentials(s.db, user) {
		log.Println("Invalid credentials")
		http.Error(w, "Method not allowed", 400)
		return
	}

	filePath := r.Form.Get("file_path")
	if !AllowedToDownloadPath(s.db, user, filePath) {
		WriteError(w, 401, "No such file at path!")
		return
	}

	fileBytes, err := ioutil.ReadFile(FILEDIR + filePath)
	Handle(err)
	b := bytes.NewBuffer(fileBytes)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Transfer-Encoding", "binary")
	w.Header().Set("Content-Length", string(len(fileBytes)))

	_, err = b.WriteTo(w)
	Handle(err)
}

func (s *Server) CompletedDownloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 400)
		return
	}

	// fetch form
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", 400)
		return
	}

	var user = User{
		UUID:    r.Form.Get("UUID"),
		UUIDKey: r.Form.Get("UUID_key"),
	}

	if !IsValidUserCredentials(s.db, user) {
		http.Error(w, "Invalid user", 401)
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
			http.Error(w, "No password for user", 402)
		} else {
			transfer.from.UUID = fromUUID

			// send user stats update to sender
			go func() {
				from := User{UUID: fromUUID}
				SetUserStats(s.db, &from)
				SendSocketMessage(SocketMessage{
					User: &from,
				}, fromUUID, true)
			}()
		}
		_, err := w.Write([]byte(password))
		Handle(err)
	}

	go CompleteTransfer(s.db, transfer, failed, false)
}
