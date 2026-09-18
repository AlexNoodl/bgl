package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"bgl/internal/modules/auth"
	"bgl/internal/platform/db"
	"bgl/internal/platform/httpx"
	"bgl/internal/platform/jobs"
	"bgl/internal/platform/logging"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

func main() {
	logger := logging.New()

	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	var pool *pgxpool.Pool
	var riverClient *river.Client[pgx.Tx]
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		p, err := db.NewPool(context.Background(), dsn)
		if err != nil {
			logger.Error("api: could not create database pool", "error", err)
			os.Exit(1)
		}
		pool = p
		defer pool.Close()

		rc, err := jobs.NewInsertClient(pool)
		if err != nil {
			logger.Error("api: could not create river insert client", "error", err)
			os.Exit(1)
		}
		riverClient = rc
	} else {
		logger.Warn("api: DATABASE_URL not set — /readyz will report not-ready until it is configured, and job enqueueing is disabled")
	}

	secureCookies := os.Getenv("COOKIE_SECURE") != "false"
	if !secureCookies {
		logger.Warn("api: COOKIE_SECURE=false — session cookies will be sent over plain HTTP, dev/LAN use only")
	}

	mux := http.NewServeMux()
	registerRoutes(mux, routesDeps{
		Pool:          pool,
		RiverClient:   riverClient,
		Logger:        logger,
		SecureCookies: secureCookies,
	})

	sessionMiddleware := auth.SessionMiddleware(auth.SessionMiddlewareDeps{Pool: pool, Logger: logger})

	server := &http.Server{
		Addr:         addr,
		Handler:      httpx.WithLogging(logger)(sessionMiddleware(mux)),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	if err := httpx.RunUntilSignal(logger, server, 10*time.Second); err != nil {
		logger.Error("api: server error", "error", err)
		os.Exit(1)
	}
}
