package main

import (
	"crypto/sha256"
	"database/sql"
	b64 "encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/getsentry/sentry-go"
	log "github.com/sirupsen/logrus"
	"gopkg.in/boj/redistore.v1"
	"math"
	"math/rand"
	"net/http"
	"os"
	"runtime"
	"strings"
)

// Handle handles errors and logs them to sentry
//func Handle(err error) {
//	if err != nil {
//		pc, _, ln, _ := runtime.Caller(1)
//		details := runtime.FuncForPC(pc)
//		log.Printf("Fatal: %s - %s %d", err.Error(), details.Name(), ln)
//
//		// log to sentry
//		sentry.CaptureException(err)
//		sentry.Flush(time.Second * 5)
//	}
//}

// UpdateErr returns an error if no rows have been effected
func UpdateErr(res sql.Result, err error) error {
	if err != nil {
		return err
	}

	rowsEffected, err := res.RowsAffected()
	if rowsEffected == 0 {
		return errors.New("no rows effected")
	}
	return err
}

// GenCode creates a random capitalized string of CodeLen and verifies it doesn't already exist
func GenCode(r *http.Request, db *sql.DB) string {
	var letters = []rune("ABCDEFGHJKMNPQRSTUVWXYZ23456789")

	for {
		b := make([]rune, codeLen)
		for i := range b {
			b[i] = letters[rand.Intn(len(letters))]
		}
		code := string(b)
		if !codeExists(r, db, code) {
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
	return b64.StdEncoding.EncodeToString(v[:])
}

// MegabytesToBytes converts MB to bytes
func MegabytesToBytes(megabytes float64) int64 {
	return int64(megabytes * 1000000.0)
}

// BytesToMegabytes converts bytes to MB
func BytesToMegabytes(bytes int64) float64 {
	return float64(bytes) / 1000000.0
}

// CreditToFileUploadSize converts user credit to user max file upload size
func CreditToFileUploadSize(credit float64) (bytes int64) {
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
func CreditToBandwidth(credit float64) (bytes int64) {
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

// WriteJSON writes interface as JSON to http.ResponseWriter
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
	LogWithSkip(r, log.ErrorLevel, 2, message)

	http.Error(w, message, code)
	w.Write([]byte(message))
}

// BytesToReadable converts bytes to a readable string (MB, GB, etc...)
func BytesToReadable(bytes int64) string {
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

func Log(r *http.Request, level log.Level, args ...interface{}) {
	LogWithSkip(r, level, 3, args...)
}

func LogWithSkip(r *http.Request, level log.Level, skip int, args ...interface{}) {
	fields := log.Fields{}
	if r != nil {
		if len(r.Header.Get("X-B3-Traceid")) > 0 {
			fields["X-B3-Traceid"] = r.Header.Get("X-B3-Traceid")
		}
	}
	if len(os.Getenv("COMMIT_HASH")) > 0 {
		fields["commit-hash"] = os.Getenv("COMMIT_HASH")[:7]
	}
	pc, _, ln, _ := runtime.Caller(skip)
	details := runtime.FuncForPC(pc)
	fields["method"] = details.Name()
	fields["method-line"] = ln

	// write log
	log.WithFields(fields).Log(level, args...)

	if level == log.ErrorLevel {
		// log to sentry
		if hub := sentry.GetHubFromContext(r.Context()); hub != nil {
			hub.WithScope(func(scope *sentry.Scope) {
				for key := range fields {
					scope.SetTag(key, fmt.Sprintf("%v", fields[key]))
				}
				hub.CaptureMessage(fmt.Sprint(args...))
			})
		}
	}
}

func StoreSession(r *http.Request, w http.ResponseWriter, s *redistore.RediStore, name string, val interface{}) error {
	session, err := s.Get(r, name)
	if err != nil {
		return err
	}

	jsonVal, err := json.Marshal(val)
	if err != nil {
		return err
	}
	session.Values[name] = jsonVal
	if err := session.Save(r, w); err != nil {
		return err
	}
	return nil
}

func GetSession(r *http.Request, s *redistore.RediStore, name string, val interface{}) (err error) {
	session, err := s.Get(r, name)
	if err != nil {
		return
	}
	err = json.Unmarshal(session.Values[name].([]byte), &val)
	return
}
