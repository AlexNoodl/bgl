package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"bgl/internal/platform/httpx"
	"bgl/internal/platform/logging"
)

func main() {
	logger := logging.New()

	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	mux := http.NewServeMux()
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

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("api: starting", "addr", addr)
		serverErr <- server.ListenAndServe()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("api: listen failed", "error", err)
			os.Exit(1)
		}
	case <-stop:
		logger.Info("api: shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logger.Error("api: graceful shutdown failed", "error", err)
		}
	}
}
