package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"io"
	"io/ioutil"
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
	_ = os.RemoveAll("foo.bar")
}

func TestCredentialHandler(t *testing.T) {
	form := url.Values{}
	UUID, _ := uuid.NewRandom()

	// create account with no UUID
	rr := PostRequest("", form, http.HandlerFunc(s.CredentialHandler))
	if rr.Code != 400 {
		t.Errorf("Got %v (%v) expected %v", rr.Code, rr.Body, 400)
	}

	// create account with no public key
	form.Add("UUID", UUID.String())
	rr = PostRequest("", form, http.HandlerFunc(s.CredentialHandler))
	if rr.Code != 401 {
		t.Errorf("Got %v (%v) expected %v", rr.Code, rr.Body, 401)
	}

	// create account with invalid public key
	form.Add("public_key", "not a key")
	rr = PostRequest("", form, http.HandlerFunc(s.CredentialHandler))
	if rr.Code != 401 {
		t.Errorf("Got %v (%v) expected %v", rr.Code, rr.Body, 401)
	}

	// create account with valid public key
	form.Del("public_key")
	form.Add("public_key", b64PubKey)
	rr = PostRequest("", form, http.HandlerFunc(s.CredentialHandler))
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

	wsheader := http.Header{}
	var headers = []struct {
		key   string
		value string
		out   bool
	}{
		{"", "", false},
		{"Uuid", form.Get("UUID"), false},
		{"Uuid_key", user.UUIDKey, false},
		{"Version", "1.0.1", true},
	}

	for _, tt := range headers {
		fmt.Println(tt.key)
		wsheader.Add(tt.key, tt.value)
		server, res, ws, err := ConnectWSSHeader(wsheader)
		fmt.Printf("%v %v", res, err)
		if err == nil != tt.out {
			println(tt.key + " " + tt.value)
			t.Errorf("got %v, wanted %v - %v %v", err == nil, tt.out, res.Status, err)
		}
		if ws != nil {
			_ = ws.Close()
			server.Close()
		}
	}
}

func InitUpload(form1 url.Values, user1 User, user2 User, fileSize int) *httptest.ResponseRecorder {
	form1.Add("UUID_key", user1.UUIDKey)
	form1.Add("code", user2.Code)
	form1.Add("filesize", strconv.Itoa(fileSize))
	return PostRequest("", form1, http.HandlerFunc(s.InitUploadHandler))
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
	user1, form1 := GenUser()
	user2, form2 := GenUser()

	// create a file
	fileSize := MegabytesToBytes(249)
	encryptedFileSize := int64(FileSizeToRNCryptorBytes(fileSize))
	f, _ := os.Create("foo.bar")
	_ = f.Truncate(encryptedFileSize)

	// initial upload
	initUploadR := InitUpload(form1, user1, user2, fileSize)
	if initUploadR.Code != 200 {
		t.Errorf("Got %v (%v) expected %v", initUploadR.Code, initUploadR.Body, 200)
	}

	// file upload
	password := RandomString(10)
	uploadR := UploadFile(f, initUploadR.Header().Get("Set-Cookie"), password)
	if uploadR.Code != 200 {
		t.Errorf("Got %v (%v) expected %v", uploadR.Code, uploadR.Body, 200)
		return
	}

	// fetch stored file path notification on server that were sent when not connected
	_, _, ws, _ := ConnectWSS(user2, form2)
	var message SocketMessage
	_ = ws.SetReadDeadline(time.Now().Add(1000 * time.Millisecond)) // add timeout
	_, mess, _ := ws.ReadMessage()
	_ = json.Unmarshal(mess, &message)
	filePath := message.Download.FilePath
	if len(path.Dir(filePath)) != USERDIRLEN {
		t.Fatalf(filePath)
	}
	ws.Close()

	// attempt to download file
	form2.Add("UUID_key", user2.UUIDKey)
	form2.Add("file_path", filePath)
	rr := PostRequest("", form2, http.HandlerFunc(s.DownloadHandler))
	if rr.Code != 200 || len(rr.Body.Bytes()) != int(encryptedFileSize) {
		t.Errorf("Got %v expected %v", uploadR.Code, 200)
		t.Errorf("Got %v expected %v", len(rr.Body.Bytes()), encryptedFileSize)
	}

	// get file hash
	hasher := sha256.New()
	_, _ = hasher.Write(rr.Body.Bytes())

	// run complete handler to receive password
	form2.Add("file_path", filePath)
	form2.Add("hash", hex.EncodeToString(hasher.Sum(nil)))
	rr2 := PostRequest("", form2, http.HandlerFunc(s.CompletedDownloadHandler))
	if rr2.Body.String() != password {
		t.Errorf("Got %v expected %v", rr2.Body.String(), password)
	}
}
