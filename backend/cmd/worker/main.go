package main

import (
	"net/http"
	"os"
	"time"

	"bgl/internal/platform/httpx"
	"bgl/internal/platform/logging"
)

func main() {
	logger := logging.New()
	logger.Info("worker: skeleton — not yet implemented (see INFRA-007)")

	addr := os.Getenv("WORKER_HEALTH_ADDR")
	if addr == "" {
		addr = ":8081"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	if err := httpx.RunUntilSignal(logger, server, 5*time.Second); err != nil {
		logger.Error("worker: server error", "error", err)
		os.Exit(1)
	}
}
