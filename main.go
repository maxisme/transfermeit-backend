package main

import (
	badger "github.com/dgraph-io/badger"
	"github.com/didip/tollbooth"
	"github.com/didip/tollbooth/limiter"
	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/robfig/cron"
	"gopkg.in/tylerb/graceful.v1"
	"log"
	"net/http"
	"os"
	"time"
)

var sentryHandler *sentryhttp.Handler = nil
var lmt = tollbooth.NewLimiter(1, &limiter.ExpirableOptions{DefaultExpirationTTL: time.Hour}).SetIPLookups([]string{
	"RemoteAddr", "X-Forwarded-For", "X-Real-IP",
})

func customCallback(nextFunc func(http.ResponseWriter, *http.Request)) http.Handler {
	var h = SecKeyHandler(tollbooth.LimitFuncHandler(lmt, nextFunc))
	if sentryHandler != nil {
		return sentryHandler.Handle(h)
	}
	return h
}

func main() {
	// SENTRY
	sentryDsn := os.Getenv("sentry_dsn")
	if sentryDsn != "" {
		if err := sentry.Init(sentry.ClientOptions{Dsn: sentryDsn}); err != nil {
			log.Fatal(err)
		}
		sentryHandler = sentryhttp.New(sentryhttp.Options{})
	}

	// create badger key store
	bdb, err := badger.Open(badger.DefaultOptions("tmp/badger"))
	if err != nil {
		log.Fatal(err)
	}
	defer bdb.Close()

	// connect to MySQL db
	db, err := DBConn(os.Getenv("db") + "?parseTime=true&loc=" + time.Local.String())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	s := Server{
		db:  db,
		bdb: bdb,
	}

	c := cron.New()
	err = c.AddFunc("@every 1m", s.CleanIncompleteUploads)
	if err != nil {
		log.Fatal(err)
	}

	// HANDLERS
	mux := http.NewServeMux()
	mux.Handle("/ws", customCallback(s.WSHandler))
	mux.Handle("/code", customCallback(s.CredentialHandler))
	mux.Handle("/init-upload", customCallback(s.InitUploadHandler))
	mux.Handle("/upload", customCallback(s.UploadHandler))
	mux.Handle("/download", customCallback(s.DownloadHandler))
	mux.Handle("/completed-download", customCallback(s.CompletedDownloadHandler))
	mux.Handle("/register", customCallback(s.RegisterCreditHandler))
	mux.Handle("/toggle-perm-code", customCallback(s.TogglePermCodeHandler))
	mux.Handle("/custom-code", customCallback(s.CustomCodeHandler))
	graceful.Run(":8080", 10*time.Minute, mux) // wait a max of 10 minute for any outstanding transfers
}
