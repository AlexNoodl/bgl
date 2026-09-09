package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"bgl/internal/platform/db"
	"bgl/internal/platform/httpx"
	"bgl/internal/platform/logging"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	logger := logging.New()

	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	var pool *pgxpool.Pool
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		p, err := db.NewPool(context.Background(), dsn)
		if err != nil {
			logger.Error("api: could not create database pool", "error", err)
			os.Exit(1)
		}
		pool = p
		defer pool.Close()
	} else {
		logger.Warn("api: DATABASE_URL not set — /readyz will report not-ready until it is configured")
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if pool == nil {
			httpx.WriteError(w, r, http.StatusServiceUnavailable, "NOT_READY", "DATABASE_URL is not configured", "")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx, pool); err != nil {
			httpx.WriteError(w, r, http.StatusServiceUnavailable, "NOT_READY", "database unavailable", "")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "no route registered yet", "")
	})

	server := &http.Server{
		Addr:         addr,
		Handler:      httpx.WithLogging(logger)(mux),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	if err := httpx.RunUntilSignal(logger, server, 10*time.Second); err != nil {
		logger.Error("api: server error", "error", err)
		os.Exit(1)
	}
}
