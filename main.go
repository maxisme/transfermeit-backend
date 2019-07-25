package main

import (
	"github.com/didip/tollbooth"
	"github.com/didip/tollbooth/limiter"
	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/labstack/gommon/log"
	"github.com/robfig/cron"
	"gopkg.in/tylerb/graceful.v1"
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
	// connect to db
	db, err := DBConn(os.Getenv("db") + "?parseTime=true")
	if err != nil {
		log.Fatal(err.Error())
	}
	defer db.Close()
	s := server{db: db}

	// SENTRY
	sentryDsn := os.Getenv("sentry_dsn")
	if sentryDsn != "" {
		if err := sentry.Init(sentry.ClientOptions{Dsn: sentryDsn}); err != nil {
			panic(err.Error())
		}
		sentryHandler = sentryhttp.New(sentryhttp.Options{})
	}

	// CRONS
	c := cron.New()
	_ = c.AddFunc("@every 1h", s.CleanIncompleteUploads)

	// HANDLERS
	mux := http.NewServeMux()
	mux.Handle("/ws", customCallback(s.WSHandler))
	mux.Handle("/code", customCallback(s.CredentialHandler))
	mux.Handle("/complete", customCallback(s.CompletedDownloadHandler))
	mux.Handle("/init-upload", customCallback(s.InitUploadHandler))
	mux.Handle("/upload", customCallback(s.UploadHandler))
	graceful.Run(":8080", 60*time.Second, mux)
}
