package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bgl/internal/modules/auth"
	"bgl/internal/modules/catalog"
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

	var mailer auth.Mailer
	if smtpHost := os.Getenv("SMTP_HOST"); smtpHost != "" {
		smtpPort := os.Getenv("SMTP_PORT")
		if smtpPort == "" {
			smtpPort = "1025" // Mailpit's default SMTP port
		}
		smtpFrom := os.Getenv("SMTP_FROM")
		if smtpFrom == "" {
			smtpFrom = "noreply@bgl.local"
		}
		webBaseURL := os.Getenv("WEB_BASE_URL")
		if webBaseURL == "" {
			webBaseURL = "http://localhost:3000"
		}
		mailer = auth.NewSMTPMailer(smtpHost+":"+smtpPort, smtpFrom, webBaseURL)
		logger.Info("worker: SMTP configured", "host", smtpHost, "port", smtpPort, "web_base_url", webBaseURL)
	} else {
		logger.Warn("worker: SMTP_HOST not set — verification emails will only be logged, not sent")
	}

	var igdbClient *catalog.IGDBClient
	if igdbClientID := os.Getenv("IGDB_CLIENT_ID"); igdbClientID != "" {
		igdbClient = catalog.NewIGDBClient(catalog.IGDBConfig{
			ClientID:     igdbClientID,
			ClientSecret: os.Getenv("IGDB_CLIENT_SECRET"),
		})
		logger.Info("worker: IGDB configured")
	} else {
		logger.Warn("worker: IGDB_CLIENT_ID not set — catalog import jobs will only be logged, not run")
	}

	riverClient, err := jobs.NewWorkerClient(pool, logger,
		auth.RegisterWorkers(logger, mailer),
		catalog.RegisterWorkers(logger, pool, igdbClient),
	)
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
