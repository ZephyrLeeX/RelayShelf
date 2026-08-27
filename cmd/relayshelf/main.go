package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/files"
	"github.com/ZephyrLeeX/RelayShelf/internal/httpapi"
	"github.com/ZephyrLeeX/RelayShelf/internal/jobs"
	"github.com/ZephyrLeeX/RelayShelf/internal/messages"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/clock"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/config"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/database"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/httpx"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/staging"
	"github.com/ZephyrLeeX/RelayShelf/internal/realtime"
	"github.com/ZephyrLeeX/RelayShelf/internal/search"
	"github.com/ZephyrLeeX/RelayShelf/internal/storage"
	"github.com/ZephyrLeeX/RelayShelf/internal/tags"
	"github.com/ZephyrLeeX/RelayShelf/internal/uploads"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type healthResponse struct {
	Status string `json:"status"`
}

type authEndpoints struct{ *auth.Handler }
type messageEndpoints struct{ *messages.Handler }
type tagEndpoints struct{ *tags.Handler }
type uploadEndpoints struct{ *uploads.Handler }
type fileEndpoints struct{ *files.Handler }
type searchEndpoints struct{ *search.Handler }
type realtimeEndpoints struct{ *realtime.Handler }
type apiHandler struct {
	*authEndpoints
	*messageEndpoints
	*tagEndpoints
	*uploadEndpoints
	*fileEndpoints
	*searchEndpoints
	*realtimeEndpoints
}

var _ httpapi.ServerInterface = (*apiHandler)(nil)

func health(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		value := "unavailable"
		if status == http.StatusOK {
			value = "ok"
		}
		_ = json.NewEncoder(w).Encode(healthResponse{Status: value})
	}
}
func ready(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil || database.CheckCompatible(ctx, db) != nil {
			health(http.StatusServiceUnavailable)(w, r)
			return
		}
		health(http.StatusOK)(w, r)
	}
}

func newHTTPRouter(apiHost func(http.Handler) http.Handler, api, live, readiness http.Handler) http.Handler {
	router := chi.NewRouter()
	router.Use(httpx.Trace, httpx.SecurityHeaders, httpx.RequestLog(log.Default()))
	router.Handle("/health/live", live)
	router.Handle("/health/ready", readiness)
	router.Group(func(router chi.Router) {
		router.Use(apiHost)
		router.Handle("/api/v1/*", api)
	})
	return router
}

func main() {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	if command == "storage" && len(os.Args) > 2 && os.Args[2] == "check" {
		storageCheck()
		return
	}
	var cfg config.Config
	var err error
	if command == "migrate" {
		cfg.DatabaseURL, err = config.LoadDatabaseURL()
	} else {
		cfg, err = config.Load()
	}
	if err != nil {
		log.Printf("invalid deployment configuration: %v", err)
		os.Exit(1)
	}
	ctx := context.Background()
	db, err := database.Open(ctx, cfg)
	if err != nil {
		log.Printf("database unavailable: %v", err)
		os.Exit(1)
	}
	defer db.Close()
	switch command {
	case "migrate":
		if err := database.Migrate(ctx, db); err != nil {
			log.Printf("migration failed: %v", err)
			os.Exit(1)
		}
		latest, _ := database.LatestVersion()
		log.Printf("migration complete; schema version %d", latest)
	case "serve":
		if err := database.CheckCompatible(ctx, db); err != nil {
			log.Printf("schema incompatible: %v", err)
			os.Exit(1)
		}
		now := clock.Real{}
		serveCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		storageAdapter, storageErr := storage.NewFilesystemStorageAdapter(cfg.StorageRoot)
		if storageErr == nil {
			_, storageErr = storage.Check(ctx, storageAdapter)
		}
		if storageErr != nil {
			log.Printf("storage unavailable at startup")
			os.Exit(1)
		}
		authRepo := auth.NewPostgreSQLRepository(db)
		hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params)
		limiter := auth.NewRateLimiter(now, auth.DefaultRateLimitEntries)
		authService := auth.NewService(authRepo, hasher, id.UUIDv7{}, now, limiter)
		csrf := auth.NewCSRF(cfg.CSRFSecret.Bytes())
		cookies := auth.NewCookiePolicy(cfg.PublicOrigin)
		authMiddleware := auth.NewMiddleware(authService, cookies, csrf, cfg.PublicOrigin, httpx.NewResolver(cfg.TrustedProxies))
		authHandler := auth.NewHandler(authService, csrf, cookies)
		bodyCipher, cipherErr := messages.NewAESGCMCipher(cfg.AppEncryptionKey.Bytes())
		if cipherErr != nil {
			log.Printf("message encryption unavailable")
			os.Exit(1)
		}
		messageRepo := messages.NewPostgreSQLRepository(db)
		messageService := messages.NewService(messageRepo, id.UUIDv7{}, now, bodyCipher)
		hub := realtime.NewHub()
		defer hub.Close()
		jobWake := jobs.NewWake()
		jobRepo := jobs.NewRepository(db, id.UUIDv7{})
		messageService.SetThumbnailJobs(jobRepo, jobWake)
		messageHandler := messages.NewHandler(messageService)
		messageHandler.SetPublisher(hub, id.UUIDv7{}, now)
		tagRepo := tags.NewPostgreSQLRepository(db)
		tagService := tags.NewService(tagRepo, id.UUIDv7{}, now)
		tagHandler := tags.NewHandler(tagService)
		tagHandler.SetPublisher(hub, id.UUIDv7{}, now)
		stagingManager, stagingErr := staging.New(cfg.StagingRoot)
		if stagingErr != nil {
			log.Printf("upload staging unavailable")
			os.Exit(1)
		}
		uploadRepo := uploads.NewPostgreSQLRepository(db)
		uploadService := uploads.NewService(uploadRepo, stagingManager, staging.NewStatFSProbe(cfg.StagingRoot), id.UUIDv7{}, now, uploads.NewLockRegistry(), cfg.MaxActiveChunkWrites, cfg.UploadStagingMaxBytes, cfg.StagingMinFreeBytes, cfg.StagingMinFreePercent)
		uploadService.SetFinalizer(uploads.NewFileFinalizer(db, storageAdapter, id.UUIDv7{}, now, cfg.FileFinalizeConcurrency))
		if cleanupErr := uploadService.ExpireDueUploads(ctx, 100); cleanupErr != nil {
			log.Printf("bounded upload expiration cleanup incomplete")
		}
		if reconcileErr := uploadService.ReconcileStaging(ctx, 1000); reconcileErr != nil {
			log.Printf("bounded upload staging reconciliation incomplete")
		}
		uploadHandler := uploads.NewHandler(uploadService)
		fileService := files.NewService(db, storageAdapter)
		if reconcileErr := fileService.Reconcile(ctx, 100); reconcileErr != nil {
			log.Printf("file object reconciliation failed")
			os.Exit(1)
		}
		if integrityErr := fileService.VerifyReady(ctx, 100); integrityErr != nil {
			log.Printf("storage integrity check failed")
			os.Exit(1)
		}
		fileHandler := files.NewHandler(fileService)
		thumbnailer := files.NewThumbnailer(db, storageAdapter, id.UUIDv7{}, now.Now)
		worker := jobs.NewWorker(jobRepo, map[string]jobs.Handler{jobs.TypeGenerateThumbnail: thumbnailer}, jobWake, now)
		var background sync.WaitGroup
		background.Add(1)
		go func() {
			defer background.Done()
			if workerErr := worker.Run(serveCtx); workerErr != nil {
				log.Printf("background worker stopped: %v", workerErr)
			}
		}()
		searchRepository := search.NewPostgreSQLRepository(db)
		searchService := search.NewService(searchRepository, now)
		searchHandler := search.NewHandler(searchService)
		realtimeHandler := realtime.NewHandler(hub, authService)
		scheduler := jobs.NewScheduler(db, jobRepo, uploadService, fileService, hub, id.UUIDv7{}, now, jobWake)
		background.Add(1)
		go func() { defer background.Done(); scheduler.Run(serveCtx) }()
		handler := &apiHandler{authEndpoints: &authEndpoints{authHandler}, messageEndpoints: &messageEndpoints{messageHandler}, tagEndpoints: &tagEndpoints{tagHandler}, uploadEndpoints: &uploadEndpoints{uploadHandler}, fileEndpoints: &fileEndpoints{fileHandler}, searchEndpoints: &searchEndpoints{searchHandler}, realtimeEndpoints: &realtimeEndpoints{realtimeHandler}}
		router := newHTTPRouter(authMiddleware.Host, auth.Router(handler, authMiddleware), health(http.StatusOK), ready(db))

		address := os.Getenv("LISTEN_ADDR")
		if address == "" {
			address = ":8080"
		}
		server := &http.Server{Addr: address, Handler: router, ReadHeaderTimeout: 10 * time.Second}
		go func() {
			<-serveCtx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
		}()
		log.Printf("relayshelf listening on %s", address)
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
		stop()
		backgroundDone := make(chan struct{})
		go func() { background.Wait(); close(backgroundDone) }()
		select {
		case <-backgroundDone:
		case <-time.After(30 * time.Second):
			log.Printf("background shutdown deadline reached")
		}
	default:
		log.Printf("unknown command %q (use serve, migrate, or storage check)", command)
		os.Exit(2)
	}
}

func storageCheck() {
	cfg, err := config.LoadStorageConfig()
	if err != nil {
		log.Printf("Storage check: FAIL (%v)", err)
		os.Exit(1)
	}
	adapter, err := storage.NewFilesystemStorageAdapter(cfg.StorageRoot)
	if err == nil {
		var result storage.CheckResult
		result, err = storage.Check(context.Background(), adapter)
		if err == nil {
			log.Printf("Storage check: PASS\nRoot: %s\nRead: PASS\nWrite: PASS\nfsync: PASS\nAtomic rename: PASS\nDelete: PASS\nSame filesystem: PASS\nAvailable bytes: %d\nTotal bytes: %d", result.Root, result.Space.AvailableBytes, result.Space.TotalBytes)
			return
		}
	}
	log.Printf("Storage check: FAIL (%v)", err)
	os.Exit(1)
}
