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

// Handle handles errors and logs them to sentry
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

// UpdateErr returns an error if no rows have been effected
func UpdateErr(res sql.Result, err error) error {
	if err != nil {
		Handle(err)
		return err
	}

	rowsEffected, err := res.RowsAffected()
	Handle(err)
	if rowsEffected == 0 {
		return errors.New("no rows effected")
	}
	return err
}

// GenCode creates a random capitalized string of CodeLen and verifies it doesn't already exist
func GenCode(db *sql.DB) string {
	var letters = []rune("ABCDEFGHJKMNPQRSTUVWXYZ23456789")

	for {
		b := make([]rune, codeLen)
		for i := range b {
			b[i] = letters[rand.Intn(len(letters))]
		}
		code := string(b)
		if !codeExists(db, code) {
			return code
		}
	}
}

// RandomString generates a random string
func RandomString(n int) string {
	var letter = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

	b := make([]rune, n)
	for i := range b {
		b[i] = letter[rand.Intn(len(letter))]
	}
	return string(b)
}

// Hash hashes a string
func Hash(str string) string {
	out, err := b64.StdEncoding.DecodeString(str)
	if err == nil && len(out) > 1 {
		// if already b64 encoded don't hash
		return str
	}
	v := sha256.Sum256([]byte(str))
	return string(b64.StdEncoding.EncodeToString(v[:]))
}

// HashWithBytes hashes bytes
func HashWithBytes(bytes []byte) string {
	hasher := sha256.New()
	_, err := hasher.Write(bytes)
	Handle(err)
	return hex.EncodeToString(hasher.Sum(nil))
}

// MegabytesToBytes converts MB to bytes
func MegabytesToBytes(megabytes float64) int {
	return int(megabytes * 1000000.0)
}

// BytesToMegabytes converts bytes to MB
func BytesToMegabytes(bytes int) float64 {
	return float64(bytes) / 1000000.0
}

// CreditToFileUploadSize converts user credit to user max file upload size
func CreditToFileUploadSize(credit float64) (bytes int) {
	bytes = freeFileUploadBytes
	for {
		if credit > 0 {
			bytes += freeFileUploadBytes
			credit -= creditSteps
			continue
		}
		break
	}
	return
}

// CreditToBandwidth converts user credit to user bandwidth
func CreditToBandwidth(credit float64) (bytes int) {
	bytes = freeBandwidthBytes
	for {
		if credit > 0 {
			bytes += freeBandwidthBytes
			credit -= creditSteps
			continue
		}
		break
	}
	return
}

// WriteJSON writes JSON response
func WriteJSON(w http.ResponseWriter, v interface{}) error {
	jsonReply, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(jsonReply)
	return err
}

// WriteError will write a http.Error as well as logging the error locally and to Sentry
func WriteError(w http.ResponseWriter, r *http.Request, code int, message string) {
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

// InitSession initiates a http session
func InitSession(r *http.Request) *sessions.Session {
	if appSession != nil {
		// there already is a global session so use that
		return appSession
	}
	session, err := store.Get(r, uploadSessionName)
	appSession = session // set global session
	Handle(err)
	return session
}

// BytesToReadable converts bytes to a readable string (MB, GB, etc...)
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
