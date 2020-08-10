package main

import (
	"database/sql"
	"github.com/TV4/graceful"
	"github.com/didip/tollbooth"
	"github.com/didip/tollbooth/limiter"
	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/go-chi/chi"
	"github.com/go-redis/redis/v7"
	"github.com/joho/godotenv"
	"github.com/maxisme/notifi-backend/conn"
	"github.com/maxisme/notifi-backend/ws"
	"github.com/minio/minio-go/v7"
	"github.com/patrickmn/go-cache"
	"github.com/robfig/cron"
	log "github.com/sirupsen/logrus"

	"net/http"
	"os"
	"time"
)

func init() {
	log.SetFormatter(&log.JSONFormatter{})
	log.SetOutput(os.Stdout)
	log.SetLevel(log.TraceLevel)
}

// ServerKeyMiddleware middleware makes sure the Sec-Key header matches the SERVER_KEY environment variable as
// well as rate limiting requests
func ServerKeyMiddleware(next http.Handler) http.Handler {
	return tollbooth.LimitFuncHandler(tollbooth.NewLimiter(
		2,
		&limiter.ExpirableOptions{DefaultExpirationTTL: time.Hour},
	).SetIPLookups([]string{"RemoteAddr", "X-Forwarded-For", "X-Real-IP"}), func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Sec-Key") != os.Getenv("SERVER_KEY") {
			WriteError(w, r, http.StatusForbidden, "Invalid server key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

var c = cache.New(1*time.Minute, 5*time.Minute)

// Server is used for database pooling - sharing the db connection to the web handlers.
type Server struct {
	db      *sql.DB
	minio   *minio.Client
	redis   *redis.Client
	funnels *ws.Funnels
}

func main() {
	// load .env
	if err := godotenv.Load(); err != nil {
		panic(err)
	}

	// SENTRY
	sentryDsn := os.Getenv("SENTRY_DSN")
	if sentryDsn != "" {
		if err := sentry.Init(sentry.ClientOptions{Dsn: sentryDsn}); err != nil {
			log.Fatal(err)
		}
	}
	sentryMiddleware := sentryhttp.New(sentryhttp.Options{})

	// connect to db
	dbConn, err := conn.DbConn(os.Getenv("DB_HOST") + "/transfermeit?parseTime=true&loc=" + time.Local.String())
	if err != nil {
		panic(err)
	}
	defer dbConn.Close()

	// connect to redis
	redisConn, err := conn.RedisConn(os.Getenv("REDIS_HOST"))
	if err != nil {
		panic(err)
	}
	defer redisConn.Close()

	// connect to minio
	minioClient, err := getMinioClient(os.Getenv("MINIO_ENDPOINT"), bucketName, os.Getenv("MINIO_ACCESS_KEY"), os.Getenv("MINIO_SECRET_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	s := Server{
		db:      dbConn,
		minio:   minioClient,
		redis:   redisConn,
		funnels: &ws.Funnels{Clients: make(map[string]*ws.Funnel), StoreOnFailure: true},
	}

	// clean up cron
	c := cron.New()
	err = c.AddFunc("@every 1m", func() {
		if err := s.CleanExpiredTransfers(); err != nil {
			log.Error(err)
		}
	})
	if err != nil {
		log.Fatal(err)
	}
	c.Start()

	r := chi.NewRouter()

	// middleware
	r.Use(sentryMiddleware.Handle)
	r.HandleFunc("/health", func(writer http.ResponseWriter, request *http.Request) {})

	mux := r
	mux.Use(ServerKeyMiddleware)

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
