package main

import (
	"bytes"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/sessions"
	"github.com/gorilla/websocket"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

var clients = make(map[string]*websocket.Conn)
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}
var (
	appSession        *sessions.Session
	store             = sessions.NewCookieStore([]byte(os.Getenv("session_key")))
	UPLOADSESSIONNAME = "upload"
)

func SecKeyHandler(next http.Handler) http.Handler {
	middle := func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Sec-Key") != SERVERKEY {
			log.Println("Invalid key", r.Header.Get("Sec-Key"))
			http.Error(w, "Invalid form data", 400)
			return
		}
		next.ServeHTTP(w, r)
	}

	return http.HandlerFunc(middle)
}

func (s *server) WSHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", 400)
		return
	}

	// validate inputs
	if !IsValidVersion(r.Header.Get("Version")) {
		http.Error(w, "Invalid Version", 400)
		return
	}

	var user = User{
		UUID:    r.Header.Get("Uuid"),
		UUIDKey: r.Header.Get("Uuid_key"),
	}

	if !IsValidUserCredentials(s.db, user) {
		WriteError(w, 401, "Invalid credentials!")
		return
	}

	// CONNECT TO SOCKET
	wsconn, _ := upgrader.Upgrade(w, r, nil)
	clients[Hash(user.UUID)] = wsconn // add conn to clients
	go UserSocketConnected(s.db, user, true)

	// SEND ALL PENDING MESSAGES
	if messages, ok := pendingSocketMessages[Hash(user.UUID)]; ok {
		for _, message := range messages {
			SendSocketMessage(message, Hash(user.UUID), false)
		}
		delete(pendingSocketMessages, Hash(user.UUID)) // delete pending messages
	}

	// INCOMING SOCKET MESSAGES
	for {
		_, message, err := wsconn.ReadMessage()
		if err != nil {
			fmt.Println(err.Error())
			break
		}

		if string(message) == "user" {
			SetUserStats(s.db, &user)
			go SendSocketMessage(SocketMessage{
				MessageType: "user",
				User:        &user,
			}, user.UUID, true)
		}
		break
	}

	go UserSocketConnected(s.db, user, false)
	delete(clients, user.UUID)
}

func (s *server) CredentialHandler(w http.ResponseWriter, r *http.Request) {
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

	user := User{
		UUID:      r.Form.Get("UUID"),
		UUIDKey:   r.Form.Get("UUID_key"),
		PublicKey: r.Form.Get("public_key"),
	}

	if user.PublicKey == "" {
		WriteError(w, 401, "Invalid form data")
		return
	}

	decodedPublicKey, err := base64.StdEncoding.DecodeString(user.PublicKey)
	if err == nil {
		re, err := x509.ParsePKIXPublicKey(decodedPublicKey)
		if err == nil {
			pub := re.(*rsa.PublicKey)
			if pub == nil {
				err = errors.New("invalid public key")
			}
		}
	}
	if err != nil {
		WriteError(w, 401, err.Error())
		return
	}

	wantedMins, err := strconv.Atoi(r.Form.Get("wanted_mins"))
	if err != nil || wantedMins <= 0 || wantedMins%5 != 0 {
		wantedMins = DEFAULTMIN
	}

	user.Code = RandomString(7)

	if !IsValidUserCredentials(s.db, user) {
		if HasUUID(s.db, user) {
			WriteError(w, 402, "Invalid UUID key")
			return
		} else {
			// create new tmi user
			user.UUIDKey = RandomString(UUIDKEYLEN)
			user.MaxFileSize = FREEFILEUPLOAD
			user.Bandwidth = FREEBANDWIDTH
			user.MinsAllowed = DEFAULTMIN
			go CreateNewUser(s.db, user)
		}
	} else {
		permCode := Hash(r.Form.Get("perm_code"))
		if permCode == getPermCode(s.db, user) {
			user.Code = permCode
		}
		SetUserMinsAllowed(&user)
	}

	if wantedMins > user.MinsAllowed {
		wantedMins = DEFAULTMIN
	}
	go UpdateUser(s.db, user, wantedMins)

	user.TimeLeft = time.Now().Add(time.Second * time.Duration(wantedMins))

	jsonReply, err := json.Marshal(user)
	Err(err)
	_, err = w.Write(jsonReply)
	Err(err)
}

func (s *server) InitUploadHandler(w http.ResponseWriter, r *http.Request) {
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
		log.Println(err.Error())
		WriteError(w, 401, "Invalid value for filesize")
		return
	}

	user := User{
		UUID:        r.Form.Get("UUID"),
		UUIDKey:     r.Form.Get("UUID_key"),
		MaxFileSize: filesize,
	}

	if !IsValidUserCredentials(s.db, user) {
		log.Println("Invalid credentials")
		http.Error(w, "Method not allowed", 400)
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

	upload := Upload{
		fromUUID: Hash(user.UUID),
		toUUID:   friend.UUID,
	}

	upload.size = FileSizeToRNCryptorBytes(user.MaxFileSize)
	SetMaxFileUpload(s.db, &user)
	fmt.Println()
	if user.MaxFileSize-upload.size < 0 {
		log.Printf("upload with %v difference", BytesToMegabytes(user.MaxFileSize-upload.size))
		mb := BytesToMegabytes(user.MaxFileSize)
		m := fmt.Sprintf("This upload exceeds your %fMB max file upload size!", mb)
		WriteError(w, 401, m)
		return
	}
	userBandwidthLeft := GetUserBandwidthLeft(s.db, &user)
	if userBandwidthLeft-upload.size < 0 {
		WriteError(w, 401, "This upload exceeds today's bandwidth limit!")
		return
	}

	if IsAlreadyUploading(s.db, &upload) {
		// already uploading to friend so delete the currently in process upload
		go CompleteUpload(s.db, upload, true)
	}

	SetUserTier(s.db, &user)
	if user.Tier > FREEUSER {
		upload.pro = true
	}

	upload.ID = InsertUpload(s.db, upload)
	upload.fromUUID = "" // for privacy reasons

	session := initSession(r)

	// store session
	gob.Register(Upload{})
	session.Values["upload"] = upload
	err = session.Save(r, w)
	Err(err)

	_, err = w.Write([]byte(friend.PublicKey))
	Err(err)
}

func (s *server) UploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", 400)
		return
	}

	_ = r.ParseMultipartForm(int64(GLOBALMAXFILESIZEMB << 20))

	// fetch form
	if err := r.ParseForm(); err != nil {
		WriteError(w, 400, "Invalid form data")
		return
	}

	// get session upload
	session := initSession(r)
	if session.Values["upload"] == nil {
		WriteError(w, 401, "Init upload not run")
		return
	}
	upload := session.Values["upload"].(Upload)
	if upload.ID == 0 {
		WriteError(w, 401, "Init upload not run")
		return
	}
	session.Values["upload"] = nil
	err := session.Save(r, w)
	Err(err)

	// get encrypted with friends public key password
	upload.password = r.Form.Get("password")

	// get file from form
	file, handler, err := r.FormFile("file")
	Err(err)
	defer file.Close()

	// verify init upload against actual file size
	if int(handler.Size) != upload.size {
		m := fmt.Sprintf("You lied about the upload size expected %v got %v!", int(handler.Size), upload.size)
		WriteError(w, 401, m)
		return
	}

	// write uploaded file
	directory := RandomString(USERDIRLEN)
	fullPath := FILEDIR + directory
	err = os.MkdirAll(fullPath, os.ModePerm)
	fileLocation, err := ioutil.TempFile(fullPath, "*.tmi")
	Err(err)
	defer fileLocation.Close()
	fileBytes, err := ioutil.ReadAll(file)
	Err(err)
	_, err = fileLocation.Write(fileBytes)
	Err(err)

	upload.FilePath = strings.Replace(fileLocation.Name(), FILEDIR, "", -1)

	// get file hash
	hasher := sha256.New()
	_, err = hasher.Write(fileBytes)
	Err(err)
	upload.hash = hex.EncodeToString(hasher.Sum(nil))

	UpdateUpload(s.db, upload)

	SendSocketMessage(SocketMessage{
		MessageType: "download",
		Download:    &upload,
	}, upload.toUUID, true)
}

func (s *server) DownloadHandler(w http.ResponseWriter, r *http.Request) {
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
	if !AllowedToDownloadPath(s.db, user.UUID, filePath) {
		WriteError(w, 401, "No such file at path!")
		return
	}

	fileBytes, err := ioutil.ReadFile(FILEDIR + filePath)
	Err(err)
	b := bytes.NewBuffer(fileBytes)

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Transfer-Encoding", "binary")
	w.Header().Set("Content-Length", string(len(fileBytes)))

	_, err = b.WriteTo(w)
	Err(err)
}

func (s *server) CompletedDownloadHandler(w http.ResponseWriter, r *http.Request) {
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

	var upload = Upload{
		toUUID:   user.UUID,
		FilePath: r.Form.Get("file_path"),
		hash:     r.Form.Get("hash"),
	}

	password := GetUploadPassword(s.db, upload)
	if password == "" {
		log.Println("No password for user. Or already uploading to user", upload)
		http.Error(w, "No password for user", 402)
		return
	}

	go CompleteUpload(s.db, upload, false)

	_, err := w.Write([]byte(password))
	Err(err)

	message := SocketMessage{
		MessageType: "message",
		Message: &Message{
			Title:   "Successful Transfer",
			Message: "Your friend successfully downloaded the file!",
		},
	}
	go SendSocketMessage(message, user.UUID, true)
}
