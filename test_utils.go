package main

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

var s server
var b64PubKey = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAvxvSoA5+YJ0dK3HFy9ccnalbqSgVGJYmQXl/1JBcN1zZGUrsBDAPRdX+TTgWbW4Ah8C+PUVmf6YbA5d+ZWmBUIYds4Ft/v2qbh3/rBEFvNw+/HhspclzwI1On6EcnylLalpF6JYYjuw4QqIJd/CsnABZwAFQ8czdtUbomic7gh9UdjkEFed5C3QqD3Nes7w7glkrEocTzwizLuxnpQZFhDEjGgONgGJSi92yf8eh0STSLGrWjT8+nw/Dw6RSWQAZviEyRtJ52WdFHIsQEAU81N5NpCr7rDPr9GHFU8sdo8Lp3fQntOIvyjpIzKUXWyp+QVJAh6GMw2Fn16S+Jg127wIDAQAB"

func InitDB(m *testing.M) {
	rand.Seed(time.Now().UTC().UnixNano())

	// initialise db
	db, err := DBConn(os.Getenv("test_db_host") + "/?multiStatements=True&parseTime=true")
	if err != nil {
		panic(err.Error())
	}
	defer db.Close()

	schema, _ := ioutil.ReadFile("sql/schema.sql")
	_, err = db.Exec(`DROP DATABASE IF EXISTS transfermeit_test; 
	CREATE DATABASE transfermeit_test;
	USE transfermeit_test; ` + string(schema))
	if err != nil {
		panic(err.Error())
	}

	db, err = DBConn(os.Getenv("db") + "?parseTime=true")
	s = server{db: db}

	code := m.Run() // RUN THE TEST

	os.Exit(code)
}

func PostRequest(url string, form url.Values, handler http.HandlerFunc) *httptest.ResponseRecorder {
	req, _ := http.NewRequest("POST", url, strings.NewReader(form.Encode()))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func GenUser() (user User, form url.Values) {
	form = url.Values{}
	UUID, _ := uuid.NewRandom()
	form.Add("UUID", UUID.String())
	form.Add("public_key", b64PubKey)
	rr := PostRequest("", form, http.HandlerFunc(s.CredentialHandler))
	_ = json.Unmarshal(rr.Body.Bytes(), &user)
	return
}

func ConnectWSS(user User, form url.Values) (*httptest.Server, *http.Response, *websocket.Conn, error) {
	wsheader := http.Header{}
	wsheader.Add("UUID", form.Get("UUID"))
	wsheader.Add("Uuid_key", user.UUIDKey)
	wsheader.Add("Version", "1.0")

	return ConnectWSSHeader(wsheader)
}

func ConnectWSSHeader(wsheader http.Header) (*httptest.Server, *http.Response, *websocket.Conn, error) {
	server := httptest.NewServer(http.HandlerFunc(s.WSHandler))
	ws, res, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), wsheader)
	if err == nil {
		_ = ws.SetReadDeadline(time.Now().Add(1000 * time.Millisecond))
	}
	return server, res, ws, err
}
