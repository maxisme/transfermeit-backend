package main

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"testing"
	"time"
)

// applied to every test
func TestMain(m *testing.M) {
	initDB(m)

	// clean up
	_ = os.Remove("./foo.bar")
	_ = os.RemoveAll("./upload")
}

func TestCredentialHandler(t *testing.T) {
	form := url.Values{}
	UUID, _ := uuid.NewRandom()

	// create account with no UUID
	rr := postRequest(form, http.HandlerFunc(s.CreateCodeHandler))
	if rr.Code != 400 {
		t.Errorf("Got %v (%v) expected %v", rr.Code, rr.Body, 400)
	}

	// create account with no public key
	form.Set("UUID", UUID.String())
	rr = postRequest(form, http.HandlerFunc(s.CreateCodeHandler))
	if rr.Code != 401 {
		t.Errorf("Got %v (%v) expected %v", rr.Code, rr.Body, 401)
	}

	// create account with invalid public key
	form.Set("public_key", "not a key")
	rr = postRequest(form, http.HandlerFunc(s.CreateCodeHandler))
	if rr.Code != 401 {
		t.Errorf("Got %v (%v) expected %v", rr.Code, rr.Body, 401)
	}

	// create account with valid public key
	form.Del("public_key")
	form.Set("public_key", testB64PubKey)
	rr = postRequest(form, http.HandlerFunc(s.CreateCodeHandler))
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
	user, form := genUser()
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
		_, res, _, err := connectWSSHeader(wsheader)
		if err == nil != tt.out {
			t.Errorf("got %v, wanted %v - %v %v %v", err == nil, tt.out, res.Status, err, wsheader)
		}
	}
}

func TestUploadDownloadCycle(t *testing.T) {
	// create two users
	user1, form1 := genUser()
	user2, form2 := genUser()
	_, _, user1Ws, _ := connectWSS(user1, form1)

	// UPLOAD
	fileSize := MegabytesToBytes(10)
	password := upload(t, user1, user2, form1, fileSize)

	// DOWNLOAD

	// fetch initial message containing download file path that was sent when user was not connected to web socket
	_, _, user2Ws, _ := connectWSS(user2, form2)
	message := readSocketMessage(user2Ws)
	filePath := message.Download.FilePath
	if len(path.Dir(filePath)) != userDirLen {
		t.Fatalf(filePath)
	}
	user2Ws.Close()

	// download file at path
	form2.Set("UUID_key", user2.UUIDKey)
	form2.Set("file_path", filePath)
	rr := postRequest(form2, http.HandlerFunc(s.DownloadHandler))
	if rr.Code != 200 || len(rr.Body.Bytes()) != fileSize {
		t.Errorf("Got %v expected %v", len(rr.Body.Bytes()), fileSize)
	}

	// run complete handler to receive password
	form2.Set("file_path", filePath)
	form2.Set("hash", HashWithBytes(rr.Body.Bytes()))
	rr2 := postRequest(form2, http.HandlerFunc(s.CompletedDownloadHandler))
	if rr2.Body.String() != password {
		t.Errorf("Got %v expected %v", rr2.Body.String(), password)
	}

	// verify file was deleted from server
	if _, err := os.Stat(fileStoreDirectory + filePath); err == nil {
		t.Errorf("file at path: '%v' should have been deleted", fileStoreDirectory+filePath)
	}

	// fetch success notification
	message = readSocketMessage(user1Ws)
	if message.Message.Title != "Successful Transfer" {
		t.Errorf("expected: %v got %v", "Successful Transfer", message.Message.Title)
	}

	// fetch updated user stats from socket
	message = readSocketMessage(user1Ws)
	if message.User.BandwidthLeft != freeBandwidthBytes-fileSize {
		t.Errorf("expected %v got %v", freeBandwidthBytes-fileSize, message.User.BandwidthLeft)
	}
}

func TestInvalidUploadFileSizeVariable(t *testing.T) {
	user1, form1 := genUser()
	form1.Set("UUID_key", user1.UUIDKey)
	rr := postRequest(form1, http.HandlerFunc(s.InitUploadHandler))
	if rr.Code != 401 { // TODO test body
		t.Errorf("expected: %d got %d - %s", 401, rr.Code, rr.Body.String())
	}
}

func TestUploadToNonExistingFriend(t *testing.T) {
	user1, form1 := genUser()
	form1.Set("UUID_key", user1.UUIDKey)
	form1.Set("filesize", strconv.Itoa(123))
	form1.Set("code", RandomString(codeLen))
	rr := postRequest(form1, http.HandlerFunc(s.InitUploadHandler))
	if rr.Code != 402 { // TODO test body
		t.Errorf("expected: %d got %d - %s", 402, rr.Code, rr.Body.String())
	}
}

func TestInvalidSendUploadToSelf(t *testing.T) {
	user1, form1 := genUser()
	form1.Set("UUID_key", user1.UUIDKey)
	form1.Set("filesize", strconv.Itoa(123))
	form1.Set("code", user1.Code)
	rr := postRequest(form1, http.HandlerFunc(s.InitUploadHandler))
	if rr.Code != 403 { // TODO test body
		t.Errorf("expected: %d got %d - %s", 403, rr.Code, rr.Body.String())
	}
}

func TestExceedBandwidth(t *testing.T) {
	// TODO
}

func TestTooLargeUpload(t *testing.T) {
	user1, form1 := genUser()
	user2, _ := genUser()
	form1.Set("UUID_key", user1.UUIDKey)
	form1.Set("filesize", strconv.Itoa(freeFileUploadBytes+1))
	form1.Set("code", user2.Code)
	rr := postRequest(form1, http.HandlerFunc(s.InitUploadHandler))
	if rr.Code != 405 { // TODO test body
		t.Errorf("expected: %d got %d - %s", 405, rr.Code, rr.Body.String())
	}
}

func TestTwoPendingTransfers(t *testing.T) {
	user1, form1 := genUser()
	user2, _ := genUser()
	_, _, ws, _ := connectWSS(user1, form1)

	_ = initUpload(form1, user1, user2, 10)
	_ = initUpload(form1, user1, user2, 10)

	m := readSocketMessage(ws)
	expected := "Cancelled Transfer"
	if m.Message.Title != expected {
		t.Errorf("expected: %s got %s", expected, m.Message.Title)
	}
}

func TestCodeTimeout(t *testing.T) {
	user, form := genUser()

	const secondHang = 3
	time.Sleep(time.Second * time.Duration(secondHang))

	_, _, ws, _ := connectWSS(user, form)

	// send stats socket message to acquire user info
	socketMessage, _ := json.Marshal(IncomingSocketMessage{Type: "stats"})
	_ = ws.WriteMessage(websocket.TextMessage, socketMessage)

	message := readSocketMessage(ws)

	secondsLeft := math.Floor(message.User.Expiry.Sub(time.Now()).Seconds())
	estimatedSecondsLeft := defaultAccountLifeMins*60 - secondHang

	if estimatedSecondsLeft != int(secondsLeft) && estimatedSecondsLeft-1 != int(secondsLeft) {
		t.Errorf("Got %v expected %v", secondsLeft, estimatedSecondsLeft)
	}
}

func TestPermCode(t *testing.T) {
	_, form := genCreditUser(permCodeCreditAmt)

	// toggle on perm code
	rr := postRequest(form, http.HandlerFunc(s.TogglePermCodeHandler))
	if rr.Code != 200 {
		t.Errorf("Got %v expected %v", rr.Body.String(), 200)
	}
	var user User
	_ = json.Unmarshal(rr.Body.Bytes(), &user)
	var permCode = user.Code

	// test that if client does not pass perm_user_code it sets a new code
	rr = postRequest(form, http.HandlerFunc(s.CreateCodeHandler))
	_ = json.Unmarshal(rr.Body.Bytes(), &user)
	if permCode == user.Code {
		t.Errorf("Should have given a new random code as perm_user_code was not passed!")
	}

	// request new code
	form.Set("perm_user_code", permCode)
	rr = postRequest(form, http.HandlerFunc(s.CreateCodeHandler))
	_ = json.Unmarshal(rr.Body.Bytes(), &user)
	if permCode != user.Code {
		t.Errorf("Should have kept same code '%v' instead of returning '%v' %v", permCode, user.Code, rr.Code)
	}
	permCode = user.Code

	// toggle off perm code
	rr = postRequest(form, http.HandlerFunc(s.TogglePermCodeHandler))
	_ = json.Unmarshal(rr.Body.Bytes(), &user)
	if permCode == user.Code {
		t.Errorf("Should have changed code %v vs %v", permCode, user.Code)
	}
}

func TestUUIDReset(t *testing.T) {
	_, form := genUser()

	removeUUIDKey(form)

	rr := postRequest(form, http.HandlerFunc(s.CreateCodeHandler))
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
	var customCode = GenCode(s.db)
	var user User

	_, form := genCreditUser(customCodeCreditAmt)

	// set a custom code
	form.Set("custom_code", customCode)
	rr := postRequest(form, http.HandlerFunc(s.CustomCodeHandler))
	_ = json.Unmarshal(rr.Body.Bytes(), &user)
	if customCode != user.Code {
		t.Errorf("Should have set custom code %v vs %v", customCode, user.Code)
	}

	// test that if client does not pass perm_user_code it sets a new code
	rr = postRequest(form, http.HandlerFunc(s.CreateCodeHandler))
	_ = json.Unmarshal(rr.Body.Bytes(), &user)
	if customCode == user.Code {
		t.Errorf("Should have given a new random code as perm_user_code was not passed!")
	}

	// test when creating a new code you are given the same custom code
	form.Set("perm_user_code", customCode)
	rr = postRequest(form, http.HandlerFunc(s.CreateCodeHandler))
	_ = json.Unmarshal(rr.Body.Bytes(), &user)
	if customCode != user.Code {
		t.Errorf("Should have set custom code %v vs %v", customCode, user.Code)
	}
}

var invalidHandlerMethods = []struct {
	handler       http.HandlerFunc
	invalidMethod string
}{
	{http.HandlerFunc(s.CompletedDownloadHandler), "GET"},
	{http.HandlerFunc(s.UploadHandler), "GET"},
	{http.HandlerFunc(s.InitUploadHandler), "GET"},
	{http.HandlerFunc(s.DownloadHandler), "GET"},
	{http.HandlerFunc(s.CreateCodeHandler), "GET"},
	{http.HandlerFunc(s.RegisterCreditHandler), "GET"},
	{http.HandlerFunc(s.CustomCodeHandler), "GET"},
	{http.HandlerFunc(s.TogglePermCodeHandler), "GET"},
	{http.HandlerFunc(s.LiveHandler), "POST"},
	{http.HandlerFunc(s.WSHandler), "POST"},
}

// request handlers with incorrect methods
func TestInvalidHandlerMethods(t *testing.T) {
	for i, tt := range invalidHandlerMethods {
		t.Run(string(i), func(t *testing.T) {
			req, _ := http.NewRequest(tt.invalidMethod, "", nil)

			rr := httptest.NewRecorder()
			tt.handler.ServeHTTP(rr, req)
			if rr.Code != 400 {
				t.Errorf("Should have responded with error code %d not %d", 400, rr.Code)
			}
		})
	}
}

var userLoginDetailsHandlers = []struct {
	handler http.HandlerFunc
}{
	{http.HandlerFunc(s.CompletedDownloadHandler)},
	{http.HandlerFunc(s.InitUploadHandler)},
	{http.HandlerFunc(s.DownloadHandler)},
	{http.HandlerFunc(s.RegisterCreditHandler)},
	{http.HandlerFunc(s.CustomCodeHandler)},
	{http.HandlerFunc(s.TogglePermCodeHandler)},
	{http.HandlerFunc(s.WSHandler)},
}

func TestInvalidIsValidUsers(t *testing.T) {
	for i, tt := range userLoginDetailsHandlers {
		t.Run(string(i), func(t *testing.T) {

			invalidUserLogin := url.Values{}
			invalidUserLogin.Set("UUID", "")
			invalidUserLogin.Set("UUID_key", "")

			req, _ := http.NewRequest("POST", "", strings.NewReader(invalidUserLogin.Encode()))

			rr := httptest.NewRecorder()
			tt.handler.ServeHTTP(rr, req)
			if rr.Code != 400 {
				t.Errorf("Should have responded with error code %d not %d", 400, rr.Code)
			}
		})
	}
}
