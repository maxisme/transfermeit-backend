package main

import (
	"github.com/TV4/graceful"
	"github.com/didip/tollbooth"
	"github.com/didip/tollbooth/limiter"
	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/patrickmn/go-cache"
	"github.com/robfig/cron"
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

var c = cache.New(1*time.Minute, 5*time.Minute)

func main() {
	// SENTRY
	sentryDsn := os.Getenv("sentry_dsn")
	if sentryDsn != "" {
		if err := sentry.Init(sentry.ClientOptions{Dsn: sentryDsn}); err != nil {
			log.Fatal(err)
		}
		sentryHandler = sentryhttp.New(sentryhttp.Options{})
	}

	// connect to MySQL db
	db, err := DBConn(os.Getenv("db") + "?parseTime=true&loc=" + time.Local.String())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	s := Server{db: db}

	// clean up cron
	c := cron.New()
	err = c.AddFunc("@every 1m", s.CleanIncompleteTransfers)
	if err != nil {
		log.Fatal(err)
	}
	c.Start()

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

	mux.HandleFunc("/live", s.liveHandler)
	graceful.ListenAndServe(&http.Server{Addr: ":8080", Handler: mux})
}
