package main

import (
	"database/sql"
	"fmt"
	"github.com/TV4/graceful"
	"github.com/didip/tollbooth"
	"github.com/didip/tollbooth/limiter"
	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/go-redis/redis/v7"
	"github.com/joho/godotenv"
	"github.com/maxisme/notifi-backend/conn"
	"github.com/maxisme/notifi-backend/ws"
	"github.com/maxisme/transfermeit-backend/tracer"
	"github.com/minio/minio-go/v7"
	log "github.com/sirupsen/logrus"
	"gopkg.in/boj/redistore.v1"

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
	).SetIPLookups([]string{"X-Real-IP"}), func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Sec-Key") != os.Getenv("SERVER_KEY") {
			WriteError(w, r, http.StatusForbidden, "Invalid server key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Server
type Server struct {
	db      *sql.DB
	minio   *minio.Client
	redis   *redis.Client
	funnels *ws.Funnels
	session *redistore.RediStore
}

func main() {
	// load .env
	_ = godotenv.Load()

	// SENTRY
	sentryDsn := os.Getenv("SENTRY_DSN")
	if sentryDsn != "" {
		if err := sentry.Init(sentry.ClientOptions{Dsn: sentryDsn}); err != nil {
			log.Fatal(err)
		}
	}
	sentryMiddleware := sentryhttp.New(sentryhttp.Options{})

	if os.Getenv("COLLECTOR_HOSTNAME") != "" {
		// start tracer
		fn, err := tracer.InitJaegerExporter("Transfer Me It", os.Getenv("COLLECTOR_HOSTNAME"))
		if err != nil {
			panic(err)
		}
		defer fn()
	}

	// connect to db
	time.Sleep(2 * time.Second)
	dbConn, err := conn.DbConn(os.Getenv("DB_HOST") + "?parseTime=true&loc=" + time.Local.String())
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

	// create redis cookie store
	redisStore, err := redistore.NewRediStore(10, "tcp", os.Getenv("REDIS_HOST"), "", []byte(os.Getenv("SESSION_KEY")))
	if err != nil {
		panic(err)
	}

	// connect to minio
	minioClient, err := getMinioClient(os.Getenv("MINIO_ENDPOINT"), bucketName, os.Getenv("MINIO_ACCESS_KEY"), os.Getenv("MINIO_SECRET_KEY"))
	if err != nil {
		panic(err)
	}

	s := Server{
		db:      dbConn,
		minio:   minioClient,
		redis:   redisConn,
		session: redisStore,
		funnels: &ws.Funnels{Clients: make(map[string]*ws.Funnel), StoreOnFailure: true},
	}

	// initiate file clean up cron TODO move to seperate service
	//c := cron.New()
	//err = c.AddFunc("@every 1m", func() {
	//	if err := s.CleanExpiredTransfers(nil); err != nil {
	//		log.Error(err)
	//	}
	//})
	//if err != nil {
	//	log.Fatal(err)
	//}
	//c.Start()

	r := chi.NewRouter()

	r.Group(func(mux chi.Router) {
		mux.HandleFunc("/health", func(writer http.ResponseWriter, request *http.Request) {})
	})

	// middleware
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(sentryMiddleware.Handle)
	r.Use(tracer.Middleware)
	r.HandleFunc("/live", s.LiveHandler)
	r.HandleFunc("/trace", func(w http.ResponseWriter, r *http.Request) {
		span := tracer.GetSpan(r, "child-span")
		_, _ = w.Write([]byte(fmt.Sprintf("%v", r.Header)))
		span.End()
	})

	r.Group(func(mux chi.Router) {
		mux.Use(ServerKeyMiddleware)

		mux.HandleFunc("/ws", s.WSHandler)
		mux.HandleFunc("/code", s.CreateCodeHandler)
		mux.HandleFunc("/init-upload", s.InitUploadHandler)
		mux.HandleFunc("/upload", s.UploadHandler)
		mux.HandleFunc("/download", s.DownloadHandler)
		mux.HandleFunc("/completed-download", s.CompletedDownloadHandler)
		mux.HandleFunc("/register", s.RegisterCreditHandler)
		mux.HandleFunc("/toggle-perm-code", s.TogglePermCodeHandler)
		mux.HandleFunc("/custom-code", s.CustomCodeHandler)
	})

	graceful.ListenAndServe(&http.Server{Addr: ":8080", Handler: r})
}
