package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"io"
	"io/ioutil"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"strconv"
	"testing"
	"time"
)

// applied to every test
func TestMain(m *testing.M) {
	InitDB(m)

	// clean up
	_ = os.Remove("./foo.bar")
	_ = os.RemoveAll("./upload")
}

func TestCredentialHandler(t *testing.T) {
	form := url.Values{}
	UUID, _ := uuid.NewRandom()

	// create account with no UUID
	rr := PostRequest(form, http.HandlerFunc(s.CredentialHandler))
	if rr.Code != 400 {
		t.Errorf("Got %v (%v) expected %v", rr.Code, rr.Body, 400)
	}

	// create account with no public key
	form.Set("UUID", UUID.String())
	rr = PostRequest(form, http.HandlerFunc(s.CredentialHandler))
	if rr.Code != 401 {
		t.Errorf("Got %v (%v) expected %v", rr.Code, rr.Body, 401)
	}

	// create account with invalid public key
	form.Set("public_key", "not a key")
	rr = PostRequest(form, http.HandlerFunc(s.CredentialHandler))
	if rr.Code != 401 {
		t.Errorf("Got %v (%v) expected %v", rr.Code, rr.Body, 401)
	}

	// create account with valid public key
	form.Del("public_key")
	form.Set("public_key", b64PubKey)
	rr = PostRequest(form, http.HandlerFunc(s.CredentialHandler))
	if rr.Code != 200 {
		t.Errorf("Got %v (%v) expected %v", rr.Code, rr.Body, 200)
	}

	// parse user
	var user User
	_ = json.Unmarshal(rr.Body.Bytes(), &user)
	if len(user.Code) != 7 {
		t.Errorf("Expected a 7 digit code and the rest. Got %v", rr.Body)
	}
}

func TestWSHandler(t *testing.T) {
	user, form := GenUser()
	time.Sleep(time.Millisecond * time.Duration(100))

	wsheader := http.Header{}
	var headers = []struct {
		key   string
		value string
		out   bool
	}{
		{"", "", false},
		{"UUID", form.Get("UUID"), false},
		{"UUID-key", user.UUIDKey, false},
		{"Version", "1.0.1", true},
	}

	for _, tt := range headers {
		wsheader.Set(tt.key, tt.value)
		_, res, _, err := ConnectWSSHeader(wsheader)
		if err == nil != tt.out {
			t.Errorf("got %v, wanted %v - %v %v %v", err == nil, tt.out, res.Status, err, wsheader)
		}
	}
}

func InitUpload(form1 url.Values, user1 User, user2 User, fileSize int) *httptest.ResponseRecorder {
	form1.Set("UUID_key", user1.UUIDKey)
	form1.Set("code", user2.Code)
	form1.Set("filesize", strconv.Itoa(fileSize))
	return PostRequest(form1, http.HandlerFunc(s.InitUploadHandler))
}

func UploadFile(f *os.File, initCookie string, pass string) *httptest.ResponseRecorder {
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	part, _ := writer.CreateFormFile("file", f.Name())
	fileContents, _ := ioutil.ReadAll(f)
	_, _ = part.Write(fileContents)
	_ = writer.WriteField("password", pass)
	_ = writer.Close()
	_, _ = io.Copy(part, f)
	req, _ := http.NewRequest("POST", "", body)
	req.Header.Set("Cookie", initCookie)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	uploadR := httptest.NewRecorder()
	http.HandlerFunc(s.UploadHandler).ServeHTTP(uploadR, req)
	return uploadR
}

func TestUploadDownloadCycle(t *testing.T) {

	// create two users
	user1, form1 := GenUser()
	user2, form2 := GenUser()
	_, _, user1Ws, _ := ConnectWSS(user1, form1)

	////////////
	// UPLOAD //
	////////////

	// create a file
	fileSize := MegabytesToBytes(10)
	encryptedFileSize := int64(fileSize)
	f, _ := os.Create("foo.bar")
	defer f.Close()
	defer os.Remove("foo.bar")
	_ = f.Truncate(encryptedFileSize)

	// initial upload to tell
	initUploadR := InitUpload(form1, user1, user2, fileSize)
	if initUploadR.Code != 200 {
		t.Errorf("Got %v (%v) expected %v", initUploadR.Code, initUploadR.Body, 200)
	} else if initUploadR.Body.String() != b64PubKey {
		t.Errorf("Got '%v' expected '%v'", initUploadR.Body, b64PubKey)
	}

	// file upload
	password := RandomString(10)
	uploadR := UploadFile(f, initUploadR.Header().Get("Set-Cookie"), password)
	if uploadR.Code != 200 {
		t.Errorf("Got %v (%v) expected %v", uploadR.Code, uploadR.Body, 200)
		return
	}

	// DOWNLOAD

	// fetch stored file path notification on Server that were sent when not connected
	_, _, user2Ws, _ := ConnectWSS(user2, form2)
	message := ReadSocketMessage(user2Ws)
	filePath := message.Download.FilePath
	if len(path.Dir(filePath)) != USERDIRLEN {
		t.Fatalf(filePath)
	}
	user2Ws.Close()

	// attempt to download file
	form2.Set("UUID_key", user2.UUIDKey)
	form2.Set("file_path", filePath)
	rr := PostRequest(form2, http.HandlerFunc(s.DownloadHandler))
	if rr.Code != 200 || len(rr.Body.Bytes()) != int(encryptedFileSize) {
		t.Errorf("Got %v expected %v", uploadR.Code, 200)
		t.Errorf("Got %v expected %v", len(rr.Body.Bytes()), encryptedFileSize)
	}

	// get file hash
	hasher := sha256.New()
	_, _ = hasher.Write(rr.Body.Bytes())

	// run complete handler to receive password
	form2.Set("file_path", filePath)
	form2.Set("hash", hex.EncodeToString(hasher.Sum(nil)))
	rr2 := PostRequest(form2, http.HandlerFunc(s.CompletedDownloadHandler))
	if rr2.Body.String() != password {
		t.Errorf("Got %v expected %v", rr2.Body.String(), password)
	}

	if _, err := os.Stat(FILEDIR + filePath); err == nil {
		t.Errorf("'%v' should have been deleted", FILEDIR+filePath)
	}

	message = ReadSocketMessage(user1Ws)
	if message.User.Bandwidth != FREEBANDWIDTH-fileSize {
		t.Errorf("expected %v got %v", FREEBANDWIDTH-fileSize, message.User.Bandwidth)
	}

	message = ReadSocketMessage(user1Ws)
	if message.Message.Title != "Successful Transfer" {
		t.Errorf("expected: %v got %v", "Successful Transfer", message.User.Bandwidth)
	}
}

func TestCodeTimeout(t *testing.T) {
	user, form := GenUser()

	var secondHang = 3
	time.Sleep(time.Second * time.Duration(secondHang))

	_, _, ws, _ := ConnectWSS(user, form)

	// send stats socket message to acquire user info
	socketMessage, _ := json.Marshal(IncomingSocketMessage{Type: "stats"})
	_ = ws.WriteMessage(websocket.TextMessage, socketMessage)

	message := ReadSocketMessage(ws)

	secondsLeft := math.Floor(message.User.Expiry.Sub(time.Now()).Seconds())
	estimatedSecondsLeft := DEFAULTMIN*60 - secondHang

	if estimatedSecondsLeft != int(secondsLeft) && estimatedSecondsLeft-1 != int(secondsLeft) {
		t.Errorf("Got %v expected %v", secondsLeft, estimatedSecondsLeft)
	}
}

func TestPermCode(t *testing.T) {
	_, form := GenCreditUser(PERMCRED)

	// toggle on perm code
	rr := PostRequest(form, http.HandlerFunc(s.TogglePermCodeHandler))
	if rr.Code != 200 {
		t.Errorf("Got %v expected %v", rr.Body.String(), 200)
	}
	var user User
	_ = json.Unmarshal(rr.Body.Bytes(), &user)
	var permCode = user.Code

	// test that if client does not pass perm_user_code it sets a new code
	rr = PostRequest(form, http.HandlerFunc(s.CredentialHandler))
	_ = json.Unmarshal(rr.Body.Bytes(), &user)
	if permCode == user.Code {
		t.Errorf("Should have given a new random code as perm_user_code was not passed!")
	}

	// request new code
	form.Set("perm_user_code", permCode)
	rr = PostRequest(form, http.HandlerFunc(s.CredentialHandler))
	_ = json.Unmarshal(rr.Body.Bytes(), &user)
	if permCode != user.Code {
		t.Errorf("Should have kept same code '%v' instead of returning '%v' %v", permCode, user.Code, rr.Code)
	}
	permCode = user.Code

	// toggle off perm code
	rr = PostRequest(form, http.HandlerFunc(s.TogglePermCodeHandler))
	_ = json.Unmarshal(rr.Body.Bytes(), &user)
	if permCode == user.Code {
		t.Errorf("Should have changed code %v vs %v", permCode, user.Code)
	}
}

func TestUUIDReset(t *testing.T) {
	_, form := GenUser()

	RemoveUUIDKey(form)

	rr := PostRequest(form, http.HandlerFunc(s.CredentialHandler))
	if rr.Code != 200 {
		t.Errorf("Got %v (%v) expected %v", rr.Code, rr.Body, 200)
	}
	// parse user
	var u User
	_ = json.Unmarshal(rr.Body.Bytes(), &u)
	if len(u.UUIDKey) == 0 {
		t.Errorf("Expected a UUID Key.")
	}
}

func TestCustomCode(t *testing.T) {
	var customCode = GenUserCode(s.db)
	var user User

	_, form := GenCreditUser(CODECRED)

	// set a custom code
	form.Set("custom_code", customCode)
	rr := PostRequest(form, http.HandlerFunc(s.CustomCodeHandler))
	_ = json.Unmarshal(rr.Body.Bytes(), &user)
	if customCode != user.Code {
		t.Errorf("Should have set custom code %v vs %v", customCode, user.Code)
	}

	// test that if client does not pass perm_user_code it sets a new code
	rr = PostRequest(form, http.HandlerFunc(s.CredentialHandler))
	_ = json.Unmarshal(rr.Body.Bytes(), &user)
	if customCode == user.Code {
		t.Errorf("Should have given a new random code as perm_user_code was not passed!")
	}

	// test when creating a new code you are given the same custom code
	form.Set("perm_user_code", customCode)
	rr = PostRequest(form, http.HandlerFunc(s.CredentialHandler))
	_ = json.Unmarshal(rr.Body.Bytes(), &user)
	if customCode != user.Code {
		t.Errorf("Should have set custom code %v vs %v", customCode, user.Code)
	}
}
