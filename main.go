package main

import (
	"github.com/TV4/graceful"
	"github.com/didip/tollbooth"
	"github.com/didip/tollbooth/limiter"
	"github.com/didip/tollbooth_chi"
	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/go-chi/chi"
	"github.com/joho/godotenv"
	"github.com/patrickmn/go-cache"
	"github.com/robfig/cron"
	"log"
	"net/http"
	"os"
	"time"
)

var lmt = tollbooth.NewLimiter(2, &limiter.ExpirableOptions{DefaultExpirationTTL: time.Hour}).SetIPLookups([]string{
	"RemoteAddr", "X-Forwarded-For", "X-Real-IP",
})

func ServerKeyHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Sec-Key") != os.Getenv("server_key") {
			WriteError(w, r, 400, "Invalid form data")
			return
		}
		next.ServeHTTP(w, r)
	})
}

var c = cache.New(1*time.Minute, 5*time.Minute)

func main() {
	// load .env
	if err := godotenv.Load(); err != nil {
		panic(err)
	}

	// SENTRY
	sentryDsn := os.Getenv("sentry_dsn")
	if sentryDsn != "" {
		if err := sentry.Init(sentry.ClientOptions{Dsn: sentryDsn}); err != nil {
			log.Fatal(err)
		}
	}
	sentryMiddleware := sentryhttp.New(sentryhttp.Options{})

	// connect to MySQL db
	db, err := dbConn(os.Getenv("db") + "/transfermeit?parseTime=true&loc=" + time.Local.String())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	s := Server{db: db}

	// clean up cron
	c := cron.New()
	err = c.AddFunc("@every 1m", s.CleanExpiredTransfers)
	if err != nil {
		log.Fatal(err)
	}
	c.Start()

	r := chi.NewRouter()

	// middleware
	r.Use(tollbooth_chi.LimitHandler(lmt))
	r.Use(sentryMiddleware.Handle)
	mux := r
	mux.Use(ServerKeyHandler)

	// HANDLERS
	mux.HandleFunc("/ws", s.WSHandler)
	mux.HandleFunc("/code", s.CreateCodeHandler)
	mux.HandleFunc("/init-upload", s.InitUploadHandler)
	mux.HandleFunc("/upload", s.UploadHandler)
	mux.HandleFunc("/download", s.DownloadHandler)
	mux.HandleFunc("/completed-download", s.CompletedDownloadHandler)
	mux.HandleFunc("/register", s.RegisterCreditHandler)
	mux.HandleFunc("/toggle-perm-code", s.TogglePermCodeHandler)
	mux.HandleFunc("/custom-code", s.CustomCodeHandler)

	r.HandleFunc("/live", s.LiveHandler)
	graceful.ListenAndServe(&http.Server{Addr: ":8080", Handler: r})
}
