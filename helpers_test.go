/*
helpers for *_test.go code
*/
package main

import (
	"encoding/json"
	"fmt"
	"github.com/Pallinder/go-randomdata"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/mysql"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"log"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

var s Server
var b64PubKey = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAvxvSoA5+YJ0dK3HFy9ccnalbqSgVGJYmQXl/1JBcN1zZGUrsBDAPRdX+TTgWbW4Ah8C+PUVmf6YbA5d+ZWmBUIYds4Ft/v2qbh3/rBEFvNw+/HhspclzwI1On6EcnylLalpF6JYYjuw4QqIJd/CsnABZwAFQ8czdtUbomic7gh9UdjkEFed5C3QqD3Nes7w7glkrEocTzwizLuxnpQZFhDEjGgONgGJSi92yf8eh0STSLGrWjT8+nw/Dw6RSWQAZviEyRtJ52WdFHIsQEAU81N5NpCr7rDPr9GHFU8sdo8Lp3fQntOIvyjpIzKUXWyp+QVJAh6GMw2Fn16S+Jg127wIDAQAB"

func InitDB(t *testing.M) {
	rand.Seed(time.Now().UTC().UnixNano())
	TESTDBNAME := "transfermeit_test"

	// initialise db
	db, err := DBConn(os.Getenv("db") + "/?multiStatements=True&loc=" + time.Local.String())
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

	db, err = DBConn(dbConnStr + "?parseTime=true&loc=" + time.Local.String())
	s = Server{db: db}

	code := t.Run() // RUN THE TEST

	os.Exit(code)
}

func PostRequest(form url.Values, handler http.HandlerFunc) *httptest.ResponseRecorder {
	req, _ := http.NewRequest("POST", "", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func GenUser() (user User, form url.Values) {
	form = make(url.Values)
	UUID, _ := uuid.NewRandom()
	form.Set("UUID", UUID.String())
	form.Set("public_key", b64PubKey)
	rr := PostRequest(form, http.HandlerFunc(s.CredentialHandler))
	if err := json.Unmarshal(rr.Body.Bytes(), &user); err != nil {
		log.Fatal(err)
	}
	time.Sleep(time.Millisecond * time.Duration(10))
	return
}

func GenCreditUser(credit float64) (user User, form url.Values) {
	user, form = GenUser()

	creditCode := RandomString(CreditCodeLen)
	GenerateProCredit(creditCode, credit)

	form.Set("UUID_key", user.UUIDKey)
	form.Set("credit_code", creditCode)
	_ = PostRequest(form, http.HandlerFunc(s.RegisterCreditHandler))
	return
}

func ConnectWSS(user User, form url.Values) (*httptest.Server, *http.Response, *websocket.Conn, error) {
	wsheader := http.Header{}
	wsheader.Set("UUID", form.Get("UUID"))
	wsheader.Set("UUID-key", user.UUIDKey)
	wsheader.Set("Version", "1.0")
	return ConnectWSSHeader(wsheader)
}

func ReadSocketMessage(ws *websocket.Conn) (message SocketMessage) {
	_, mess, err := ws.ReadMessage()
	if err != nil {
		Handle(err)
		return
	}
	_ = json.Unmarshal(mess, &message)
	return
}

func ConnectWSSHeader(wsheader http.Header) (*httptest.Server, *http.Response, *websocket.Conn, error) {
	server := httptest.NewServer(http.HandlerFunc(s.WSHandler))
	ws, res, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), wsheader)
	Handle(err)
	if err == nil {
		// add ws read timeout
		_ = ws.SetReadDeadline(time.Now().Add(1000 * time.Millisecond))
	}
	return server, res, ws, err
}

func GenerateProCredit(activationCode string, credit float64) {
	_, err := s.db.Exec(`
	INSERT INTO credit (activation_dttm, activation_code, credit, email)
	VALUES (NOW(), ?, ?, ?)
	`, activationCode, credit, randomdata.Email())
	Handle(err)
}

func RemoveUUIDKey(form url.Values) {
	Handle(UpdateErr(s.db.Exec(`
	UPDATE user 
	SET UUID_key=''
	WHERE UUID = ?
	`, Hash(form.Get("UUID")))))
}
