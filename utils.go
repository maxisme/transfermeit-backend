package main

import (
	"crypto/sha256"
	"database/sql"
	b64 "encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/getsentry/sentry-go"
	"github.com/gorilla/sessions"
	"github.com/gorilla/websocket"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"
)

var (
	appSession *sessions.Session
	store      = sessions.NewCookieStore([]byte(os.Getenv("session_key")))
)

func Handle(err error) {
	if err != nil {
		pc, _, ln, _ := runtime.Caller(1)
		details := runtime.FuncForPC(pc)
		log.Printf("Fatal: %s - %s %d", err.Error(), details.Name(), ln)

		// log to sentry
		sentry.CaptureException(err)
		sentry.Flush(time.Second * 5)
	}
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

func CreditToFileUploadSize(credit float64) (bytes int) {
	bytes = FreeFileUploadBytes
	for {
		if credit > 0 {
			bytes += FreeFileUploadBytes
			credit -= CreditSteps
			continue
		}
		break
	}
	return
}

func CreditToBandwidth(credit float64) (bytes int) {
	bytes = FreeBandwidthBytes
	for {
		if credit > 0 {
			bytes += FreeBandwidthBytes
			credit -= CreditSteps
			continue
		}
		break
	}
	return
}

func writeError(w http.ResponseWriter, r *http.Request, code int, message string) {
	// find where this function has been called from
	pc, _, line, _ := runtime.Caller(1)
	details := runtime.FuncForPC(pc)
	calledFrom := fmt.Sprintf("%s line:%d", details.Name(), line)

	log.Printf("HTTP error: message: %s code: %d from:%s \n", message, code, calledFrom)

	// log to sentry
	if hub := sentry.GetHubFromContext(r.Context()); hub != nil {
		hub.WithScope(func(scope *sentry.Scope) {
			scope.SetExtra("Called From", calledFrom)
			scope.SetExtra("Header Code", code)
			hub.CaptureMessage(message)
		})
	}

	http.Error(w, message, code)
	w.Write([]byte(message))
}

func SendSocketMessage(message SocketMessage, UUID string, storeOnFail bool) bool {
	hashUUID := Hash(UUID)

	WSClientsMutex.RLock()
	socket, ok := WSClients[hashUUID]
	WSClientsMutex.RUnlock()

	if ok {
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
		PendingSocketMutex.Lock()
		PendingSocketMessages[hashUUID] = append(PendingSocketMessages[hashUUID], message)
		PendingSocketMutex.Unlock()
	}

	return false
}

func InitSession(r *http.Request) *sessions.Session {
	if appSession != nil {
		return appSession
	}
	session, err := store.Get(r, UploadSessionName)
	appSession = session
	Handle(err)
	return session
}

func BytesToReadable(bytes int) string {
	if bytes == 0 {
		return "0 bytes"
	}

	base := math.Floor(math.Log(float64(bytes)) / math.Log(1000))
	units := []string{"bytes", "KB", "MB", "GB"}

	stringVal := fmt.Sprintf("%.2f", float64(bytes)/math.Pow(1000, base))
	stringVal = strings.TrimSuffix(stringVal, ".00")
	return fmt.Sprintf("%s %v",
		stringVal,
		units[int(base)],
	)
}

func HashBytes(bytes []byte) string {
	hasher := sha256.New()
	_, err := hasher.Write(bytes)
	Handle(err)
	return hex.EncodeToString(hasher.Sum(nil))
}
