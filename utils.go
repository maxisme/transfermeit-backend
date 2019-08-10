package main

import (
	"crypto/sha256"
	"database/sql"
	b64 "encoding/base64"
	"encoding/json"
	"errors"
	"github.com/gorilla/sessions"
	"github.com/gorilla/websocket"
	"log"
	"math/rand"
	"net/http"
	"os"
	"runtime"
)

var (
	appSession        *sessions.Session
	store             = sessions.NewCookieStore([]byte(os.Getenv("session_key")))
	UPLOADSESSIONNAME = "upload"
)

func Handle(err error) {
	if err != nil {
		pc, _, _, _ := runtime.Caller(1)
		details := runtime.FuncForPC(pc)
		log.Println("Fatal: " + err.Error() + " - " + details.Name())
	}
}

func RandomString(n int) string {
	var letter = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

	b := make([]rune, n)
	for i := range b {
		b[i] = letter[rand.Intn(len(letter))]
	}
	return string(b)
}

func Hash(str string) string {
	out, err := b64.StdEncoding.DecodeString(str)
	if err == nil && len(out) > 1 {
		// if already b64 encoded don't hash
		return str
	}
	v := sha256.Sum256([]byte(str))
	return string(b64.StdEncoding.EncodeToString(v[:]))
}

func MegabytesToBytes(megabytes float64) int {
	return int(megabytes * 1000000)
}

func BytesToMegabytes(bytes int) float64 {
	return float64(bytes / 1000000)
}

func CreditToFileUpload(credit float64) (bytes int) {
	bytes = FREEFILEUPLOAD
	for {
		if credit > 0 {
			bytes += FREEFILEUPLOAD
			credit -= CREDITSTEPS
			continue
		}
		break
	}
	return
}

func CreditToBandwidth(credit float64) (bytes int) {
	bytes = FREEBANDWIDTH
	for {
		if credit > 0 {
			bytes += FREEBANDWIDTH
			credit -= CREDITSTEPS
			continue
		}
		break
	}
	return
}

func WriteError(w http.ResponseWriter, code int, message string) {
	w.WriteHeader(code)
	log.Println("http error:" + message)
	_, err := w.Write([]byte(message))
	Handle(err)
}

// calculate the Size of a file after is has been encrypted with aes on the client side with RNCryptor
func FileSizeToRNCryptorBytes(bytes int) int {
	overhead := 66
	if bytes == 0 {
		return overhead + 16
	}
	remainder := bytes % 16
	if remainder == 0 {
		return bytes + 16 + overhead
	}
	return bytes + 16 + overhead - remainder
}

func SendSocketMessage(message SocketMessage, UUID string, storeOnFail bool) bool {
	hashUUID := Hash(UUID)
	if socket, ok := clients[hashUUID]; ok {
		jsonReply, err := json.Marshal(message)
		Handle(err)
		if err = socket.WriteMessage(websocket.TextMessage, jsonReply); err == nil {
			// successfully sent socket message
			return true
		} else {
			Handle(err)
		}

	} else {
		log.Println("No such UUID: " + hashUUID)
	}

	if storeOnFail {
		pendingSocketMessages[hashUUID] = append(pendingSocketMessages[hashUUID], message)
	}

	return false
}

func InitSession(r *http.Request) *sessions.Session {
	if appSession != nil {
		return appSession
	}
	session, err := store.Get(r, UPLOADSESSIONNAME)
	appSession = session
	Handle(err)
	return session
}

func UpdateErr(res sql.Result, err error) error {
	Handle(err)
	if err != nil {
		return err
	}

	rowsEffected, err := res.RowsAffected()
	Handle(err)
	if rowsEffected == 0 {
		return errors.New("no rows effected")
	}
	return err
}
