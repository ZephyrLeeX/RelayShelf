package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ZephyrLeeX/RelayShelf/internal/auth"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/clock"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/config"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/database"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/httpx"
	"github.com/ZephyrLeeX/RelayShelf/internal/platform/id"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type healthResponse struct {
	Status string `json:"status"`
}

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
		authRepo := auth.NewPostgreSQLRepository(db)
		hasher := auth.NewPasswordHasher(auth.DefaultArgon2Params)
		limiter := auth.NewRateLimiter(now, auth.DefaultRateLimitEntries)
		authService := auth.NewService(authRepo, hasher, id.UUIDv7{}, now, limiter)
		csrf := auth.NewCSRF(cfg.CSRFSecret.Bytes())
		cookies := auth.NewCookiePolicy(cfg.PublicOrigin)
		authMiddleware := auth.NewMiddleware(authService, cookies, csrf, cfg.PublicOrigin, httpx.NewResolver(cfg.TrustedProxies))
		authHandler := auth.NewHandler(authService, csrf, cookies)
		router := newHTTPRouter(authMiddleware.Host, auth.Router(authHandler, authMiddleware), health(http.StatusOK), ready(db))

		address := os.Getenv("LISTEN_ADDR")
		if address == "" {
			address = ":8080"
		}
		log.Printf("relayshelf listening on %s", address)
		if err := http.ListenAndServe(address, router); !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	default:
		log.Printf("unknown command %q (use serve or migrate)", command)
		os.Exit(2)
	}
}
