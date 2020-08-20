/*
helpers for *_test.go code
*/
package main

import (
	"github.com/Pallinder/go-randomdata"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/maxisme/notifi-backend/conn"
	"github.com/maxisme/notifi-backend/ws"
	"gopkg.in/boj/redistore.v1"

	"bytes"
	"encoding/json"
	"fmt"
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
	TESTDBNAME := "transfermeit"

	// initialise db
	db, err := conn.DbConn("root:root@tcp(127.0.0.1:3306)/?multiStatements=True&loc=" + time.Local.String())
	if err != nil {
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
	dbConnStr := "root:root@tcp(127.0.0.1:3306)/" + TESTDBNAME
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

	db, err = conn.DbConn(dbConnStr + "?parseTime=true&loc=" + time.Local.String())
	if err != nil {
		panic(err)
	}
	minio, err := getMinioClient("127.0.0.1:9000", bucketName, "FfizvUST5eqWY0jhAulGKcIdLOz09VyvfE5", "lhFC48WhyPqVPiQJvthIbdj22KnturqaayT")
	if err != nil {
		panic(err)
	}
	redis, err := conn.RedisConn("127.0.0.1:6379")
	if err != nil {
		panic(err)
	} // create redis cookie store
	redisStore, err := redistore.NewRediStore(10, "tcp", "127.0.0.1:6379", "", []byte("rps2P8irs0mT5uCgicv8m5PMq9a6WyzbxL7HWeRK"))
	if err != nil {
		panic(err)
	}

	s = Server{db: db, minio: minio, redis: redis, funnels: &ws.Funnels{StoreOnFailure: true, Clients: make(map[string]*ws.Funnel)}, session: redisStore}

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

func createUser() (user User, form url.Values) {
	form = url.Values{}
	UUID, _ := uuid.NewRandom()
	form.Set("UUID", UUID.String())
	form.Set("public_key", testB64PubKey)
	rr := postRequest(form, s.CreateCodeHandler)
	if err := json.Unmarshal(rr.Body.Bytes(), &user); err != nil {
		log.Fatal(err)
	}
	user.UUID = UUID.String()
	time.Sleep(time.Millisecond * time.Duration(10))
	return
}

func genCreditUser(credit float64) (user User, form url.Values) {
	user, form = createUser()

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
		fmt.Printf("%v\n", err)
		return
	}
	err = json.Unmarshal(mess, &message)
	if err != nil {
		panic(err)
	}
	return
}

func connectWSSHeader(wsheader http.Header) (*httptest.Server, *http.Response, *websocket.Conn, error) {
	server := httptest.NewServer(http.HandlerFunc(s.WSHandler))
	ws, res, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), wsheader)
	if err == nil {
		// add ws read timeout
		_ = ws.SetReadDeadline(time.Now().Add(1 * time.Second))
	}
	return server, res, ws, err
}

func generateProCredit(activationCode string, credit float64) {
	_, _ = s.db.Exec(`
	INSERT INTO credit (activation_dttm, activation_code, credit, email)
	VALUES (NOW(), ?, ?, ?)
	`, activationCode, credit, randomdata.Email())
}

func removeUUIDKey(form url.Values) {
	s.db.Exec(`
	UPDATE user 
	SET UUID_key=''
	WHERE UUID = ?
	`, Hash(form.Get("UUID")))
}

func markTransferAsExpired(toUUID string) {
	s.db.Exec(`
	UPDATE transfer 
	SET updated_dttm = DATE_SUB(NOW(), INTERVAL 1 HOUR), expiry_dttm = DATE_SUB(NOW(), INTERVAL 10 MINUTE)
	WHERE to_UUID = ?
	`, Hash(toUUID))
}

func getTransferUpdateDttm(toUUID string) time.Time {
	row := s.db.QueryRow(`
	SELECT updated_dttm from transfer WHERE to_UUID = ?
	`, Hash(toUUID))
	var ts time.Time
	row.Scan(&ts)
	return ts
}

func upload(t *testing.T, user1 User, user2 User, form1 url.Values, fileSize int64) string {

	////////////
	// UPLOAD //
	////////////

	// create a file
	f, _ := os.Create("foo.bar")
	defer f.Close()
	defer os.Remove("foo.bar")
	_ = f.Truncate(fileSize)

	// initial upload handler
	initUploadR := initUpload(form1, user1, user2, fileSize)
	if initUploadR.Code != 200 {
		t.Errorf("Got %v (%v) expected %v", initUploadR.Code, initUploadR.Body, 200)
	} else if initUploadR.Body.String() != testB64PubKey {
		t.Errorf("Got '%v' expected '%v'", initUploadR.Body, testB64PubKey)
	}

	// file upload handler
	password := RandomString(10)
	uploadR := uploadFile(f, initUploadR.Header().Get("Set-Cookie"), password)
	if uploadR.Code != 200 {
		t.Errorf("Got %v (%v) expected %v", uploadR.Code, uploadR.Body, 200)
	}

	return password
}

func initUpload(form1 url.Values, user1 User, user2 User, fileSize int64) *httptest.ResponseRecorder {
	form1.Set("UUID_key", user1.UUIDKey)
	form1.Set("code", user2.Code)
	form1.Set("filesize", strconv.FormatInt(fileSize, 10))
	return postRequest(form1, s.InitUploadHandler)
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
