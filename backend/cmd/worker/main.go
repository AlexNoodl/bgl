package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bgl/internal/platform/db"
	"bgl/internal/platform/jobs"
	"bgl/internal/platform/logging"
)

func main() {
	logger := logging.New()
	logger.Info("worker: starting")

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		logger.Error("worker: DATABASE_URL is required")
		os.Exit(1)
	}

	pool, err := db.NewPool(context.Background(), dsn)
	if err != nil {
		logger.Error("worker: could not create database pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	riverClient, err := jobs.NewWorkerClient(pool, logger)
	if err != nil {
		logger.Error("worker: could not create river client", "error", err)
		os.Exit(1)
	}

	if err := riverClient.Start(context.Background()); err != nil {
		logger.Error("worker: could not start river client", "error", err)
		os.Exit(1)
	}

	healthAddr := os.Getenv("WORKER_HEALTH_ADDR")
	if healthAddr == "" {
		healthAddr = ":8081"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	server := &http.Server{
		Addr:         healthAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("worker: health endpoint listening", "addr", healthAddr)
		serverErr <- server.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("worker: health listener failed", "error", err)
		}
	case <-stop:
		logger.Info("worker: shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error("worker: health listener shutdown failed", "error", err)
	}
	if err := riverClient.Stop(shutdownCtx); err != nil {
		logger.Error("worker: river client shutdown failed", "error", err)
	}
}
