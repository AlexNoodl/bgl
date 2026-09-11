package main

import (
	"context"
	"encoding/json"
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

	mux.HandleFunc("/internal/debug/enqueue-test-job", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST", "")
			return
		}
		if riverClient == nil {
			httpx.WriteError(w, r, http.StatusServiceUnavailable, "NOT_READY", "DATABASE_URL is not configured", "")
			return
		}

		message := r.URL.Query().Get("message")
		if message == "" {
			message = "hello from cmd/api"
		}

		result, err := riverClient.Insert(r.Context(), jobs.TestJobArgs{Message: message}, nil)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, "JOB_ENQUEUE_FAILED", err.Error(), "")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"job_id": result.Job.ID,
			"kind":   result.Job.Kind,
		})
	})

	mux.HandleFunc("/v1/auth/register", auth.RegisterHandler(auth.RegisterDeps{
		Pool:   pool,
		Jobs:   riverClient,
		Logger: logger,
	}))
	mux.HandleFunc("/v1/auth/verify-email", auth.VerifyEmailHandler(auth.VerifyEmailDeps{
		Pool:   pool,
		Logger: logger,
	}))

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
