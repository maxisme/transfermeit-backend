/*
helpers for *_test.go code
*/
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Pallinder/go-randomdata"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

var s Server

const testB64PubKey = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAvxvSoA5+YJ0dK3HFy9ccnalbqSgVGJYmQXl/1JBcN1zZGUrsBDAPRdX+TTgWbW4Ah8C+PUVmf6YbA5d+ZWmBUIYds4Ft/v2qbh3/rBEFvNw+/HhspclzwI1On6EcnylLalpF6JYYjuw4QqIJd/CsnABZwAFQ8czdtUbomic7gh9UdjkEFed5C3QqD3Nes7w7glkrEocTzwizLuxnpQZFhDEjGgONgGJSi92yf8eh0STSLGrWjT8+nw/Dw6RSWQAZviEyRtJ52WdFHIsQEAU81N5NpCr7rDPr9GHFU8sdo8Lp3fQntOIvyjpIzKUXWyp+QVJAh6GMw2Fn16S+Jg127wIDAQAB"

func initDB(t *testing.M) {
	rand.Seed(time.Now().UTC().UnixNano())
	TESTDBNAME := "transfermeit_test"

	// initialise db
	db, err := dbConn(os.Getenv("db") + "/?multiStatements=True&loc=" + time.Local.String())
	if err != nil {
		fmt.Println(os.Getenv("db"))
		panic(err.Error())
	}
	defer db.Close()

	_, err = db.Exec(fmt.Sprintf(`DROP DATABASE IF EXISTS %[1]v; 
	CREATE DATABASE %[1]v;`, TESTDBNAME))
	if err != nil {
		panic(err)
	}
	db.Close()

	// apply patches
	dbConnStr := os.Getenv("db") + "/" + TESTDBNAME
	m, err := migrate.New("file://sql/", "mysql://"+dbConnStr)
	if err != nil {
		panic(err)
	}

	// test up and down commands work
	if err := m.Up(); err != nil {
		panic(err)
	}
	if err := m.Down(); err != nil {
		panic(err)
	}
	if err := m.Up(); err != nil {
		panic(err)
	}

	db, err = dbConn(dbConnStr + "?parseTime=true&loc=" + time.Local.String())
	s = Server{db: db}

	code := t.Run() // RUN THE TEST

	os.Exit(code)
}

func postRequest(form url.Values, handler http.HandlerFunc) *httptest.ResponseRecorder {
	req, _ := http.NewRequest("POST", "", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func genUser() (user User, form url.Values) {
	form = url.Values{}
	UUID, _ := uuid.NewRandom()
	form.Set("UUID", UUID.String())
	form.Set("public_key", testB64PubKey)
	rr := postRequest(form, http.HandlerFunc(s.CreateCodeHandler))
	if err := json.Unmarshal(rr.Body.Bytes(), &user); err != nil {
		log.Fatal(err)
	}
	time.Sleep(time.Millisecond * time.Duration(10))
	return
}

func genCreditUser(credit float64) (user User, form url.Values) {
	user, form = genUser()

	creditCode := RandomString(CreditCodeLen)
	generateProCredit(creditCode, credit)

	form.Set("UUID_key", user.UUIDKey)
	form.Set("credit_code", creditCode)
	_ = postRequest(form, http.HandlerFunc(s.RegisterCreditHandler))
	return
}

func connectWSS(user User, form url.Values) (*httptest.Server, *http.Response, *websocket.Conn, error) {
	wsheader := http.Header{}
	wsheader.Set("UUID", form.Get("UUID"))
	wsheader.Set("UUID-key", user.UUIDKey)
	wsheader.Set("Version", "1.0")
	return connectWSSHeader(wsheader)
}

func readSocketMessage(ws *websocket.Conn) (message SocketMessage) {
	_, mess, err := ws.ReadMessage()
	if err != nil {
		Handle(err)
		return
	}
	_ = json.Unmarshal(mess, &message)
	return
}

func connectWSSHeader(wsheader http.Header) (*httptest.Server, *http.Response, *websocket.Conn, error) {
	server := httptest.NewServer(http.HandlerFunc(s.WSHandler))
	ws, res, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), wsheader)
	Handle(err)
	if err == nil {
		// add ws read timeout
		_ = ws.SetReadDeadline(time.Now().Add(5000 * time.Millisecond))
	}
	return server, res, ws, err
}

func generateProCredit(activationCode string, credit float64) {
	_, err := s.db.Exec(`
	INSERT INTO credit (activation_dttm, activation_code, credit, email)
	VALUES (NOW(), ?, ?, ?)
	`, activationCode, credit, randomdata.Email())
	Handle(err)
}

func removeUUIDKey(form url.Values) {
	Handle(UpdateErr(s.db.Exec(`
	UPDATE user 
	SET UUID_key=''
	WHERE UUID = ?
	`, Hash(form.Get("UUID")))))
}

func upload(t *testing.T, user1 User, user2 User, form1 url.Values, fileSize int) string {

	////////////
	// UPLOAD //
	////////////

	// create a file
	f, _ := os.Create("foo.bar")
	defer f.Close()
	defer os.Remove("foo.bar")
	_ = f.Truncate(int64(fileSize))

	// initial upload handler
	initUploadR := initUpload(form1, user1, user2, fileSize)
	if initUploadR.Code != 200 {
		t.Errorf("Got %v (%v) expected %v", initUploadR.Code, initUploadR.Body, 200)
	} else if initUploadR.Body.String() != testB64PubKey {
		t.Errorf("Got '%v' expected '%v'", initUploadR.Body, testB64PubKey)
	}

	// actual file upload handler
	password := RandomString(10)
	uploadR := uploadFile(f, initUploadR.Header().Get("Set-Cookie"), password)
	if uploadR.Code != 200 {
		t.Errorf("Got %v (%v) expected %v", uploadR.Code, uploadR.Body, 200)
	}

	return password
}

func initUpload(form1 url.Values, user1 User, user2 User, fileSize int) *httptest.ResponseRecorder {
	form1.Set("UUID_key", user1.UUIDKey)
	form1.Set("code", user2.Code)
	form1.Set("filesize", strconv.Itoa(fileSize))
	return postRequest(form1, http.HandlerFunc(s.InitUploadHandler))
}

func uploadFile(f *os.File, initCookie string, pass string) *httptest.ResponseRecorder {
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
