package main

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/maxisme/notifi-backend/ws"
	"github.com/minio/minio-go/v7"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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
	rr = postRequest(form, s.CreateCodeHandler)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Got %v (%v) expected %v", rr.Code, rr.Body, http.StatusBadRequest)
	}

	// create account with invalid public key
	form.Set("public_key", "not a key")
	rr = postRequest(form, s.CreateCodeHandler)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("Got %v (%v) expected %v", rr.Code, rr.Body, http.StatusBadRequest)
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
	user, form := createUser()
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
	user1, form1 := createUser()
	user2, form2 := createUser()
	_, _, user1Ws, _ := connectWSS(user1, form1)
	funnel := &ws.Funnel{
		Key:    user1.UUID,
		WSConn: user1Ws,
		PubSub: s.redis.Subscribe(user1.UUID),
	}
	s.funnels.Add(s.redis, funnel)
	_, _, user2Ws, _ := connectWSS(user2, form2)
	funnel2 := &ws.Funnel{
		Key:    user2.UUID,
		WSConn: user2Ws,
		PubSub: s.redis.Subscribe(user2.UUID),
	}
	s.funnels.Add(s.redis, funnel2)
	defer s.funnels.Remove(funnel)
	defer s.funnels.Remove(funnel2)

	// UPLOAD
	fileSize := MegabytesToBytes(10)
	password := upload(t, user1, user2, form1, fileSize)

	// DOWNLOAD

	// fetch initial message containing download file path that was sent when user was not connected to web socket

	message := readSocketMessage(funnel2.WSConn)
	objectName := message.ObjectPath
	user2Ws.Close()

	// download file at path
	form2.Set("UUID_key", user2.UUIDKey)
	form2.Set("object_name", objectName)
	rr := postRequest(form2, s.DownloadHandler)
	if rr.Code != 200 || int64(len(rr.Body.Bytes())) != fileSize {
		t.Errorf("Got %v expected %v", len(rr.Body.Bytes()), fileSize)
		return
	}

	// run complete handler to receive password
	form2.Set("object_name", objectName)
	rr2 := postRequest(form2, s.CompletedDownloadHandler)
	if rr2.Body.String() != password {
		t.Errorf("Got %v expected %v", rr2.Body.String(), password)
		return
	}

	// verify file was deleted
	obj, _ := s.minio.GetObject(context.Background(), bucketName, objectName, minio.GetObjectOptions{})
	stat, _ := obj.Stat()
	if stat.Size > 0 {
		t.Errorf("object at path: '%v' should have been deleted - %v", objectName, stat)
		return
	}

	// fetch updated user stats from socket
	message = readSocketMessage(user1Ws)
	if message.User.BandwidthLeft != freeBandwidthBytes-fileSize {
		t.Errorf("expected %v got %v", freeBandwidthBytes-fileSize, message.User.BandwidthLeft)
		return
	}

	// fetch success notification
	message = readSocketMessage(user1Ws)
	if message.Message.Title != "Successful Transfer" {
		t.Errorf("expected: %v got %v", "Successful Transfer", message.Message.Title)
		return
	}
}

func TestInvalidUploadFileSizeVariable(t *testing.T) {
	user1, form1 := createUser()
	form1.Set("UUID_key", user1.UUIDKey)
	rr := postRequest(form1, s.InitUploadHandler)
	if rr.Code != InvalidFileSizeErrorCode {
		t.Errorf("expected: %d got %d - %s", InvalidFileSizeErrorCode, rr.Code, rr.Body.String())
	}
}

func TestUploadToNonExistingFriend(t *testing.T) {
	user1, form1 := createUser()
	form1.Set("UUID_key", user1.UUIDKey)
	form1.Set("filesize", strconv.Itoa(123))
	form1.Set("code", RandomString(codeLen))
	rr := postRequest(form1, s.InitUploadHandler)
	if rr.Code != InvalidFriendErrorCode {
		t.Errorf("expected: %d got %d - %s", InvalidFriendErrorCode, rr.Code, rr.Body.String())
	}
}

func TestInvalidSendUploadToSelf(t *testing.T) {
	user1, form1 := createUser()
	form1.Set("UUID_key", user1.UUIDKey)
	form1.Set("filesize", strconv.Itoa(123))
	form1.Set("code", user1.Code)
	rr := postRequest(form1, s.InitUploadHandler)
	if rr.Code != SendToSelfErrorCode {
		t.Errorf("expected: %d got %d - %s", SendToSelfErrorCode, rr.Code, rr.Body.String())
	}
}

func TestExceedBandwidth(t *testing.T) {
	// TODO
}

func TestTooLargeUpload(t *testing.T) {
	user1, form1 := createUser()
	user2, _ := createUser()
	form1.Set("UUID_key", user1.UUIDKey)
	form1.Set("filesize", strconv.Itoa(freeFileUploadBytes+1))
	form1.Set("code", user2.Code)
	rr := postRequest(form1, s.InitUploadHandler)
	if rr.Code != FileSizeLimitErrorCode {
		t.Errorf("expected: %d got %d - %s", FileSizeLimitErrorCode, rr.Code, rr.Body.String())
	}
}

func TestTwoPendingTransfers(t *testing.T) {
	user1, form1 := createUser()
	user2, _ := createUser()
	_, _, user1Ws, _ := connectWSS(user1, form1)
	funnel := &ws.Funnel{
		Key:    user1.UUID,
		WSConn: user1Ws,
		PubSub: s.redis.Subscribe(user1.UUID),
	}
	s.funnels.Add(s.redis, funnel)

	_ = initUpload(form1, user1, user2, 10)
	_ = initUpload(form1, user1, user2, 10)

	m := readSocketMessage(user1Ws)
	expected := "Cancelled Transfer"
	if m.Message.Title != expected {
		t.Errorf("expected: %s got %s", expected, m.Message.Title)
	}
}

func TestCodeTimeout(t *testing.T) {
	user, form := createUser()

	const secondHang = 1
	time.Sleep(time.Second * time.Duration(secondHang))

	_, _, ws, _ := connectWSS(user, form)

	// send stats socket message to acquire user info
	socketMessage, _ := json.Marshal(ClientSocketMessage{Type: "stats"})
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
	_, form := createUser()

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
	var customCode = GenCode(nil, s.db)
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

func TestKeepAlive(t *testing.T) {
	//TODO
}
