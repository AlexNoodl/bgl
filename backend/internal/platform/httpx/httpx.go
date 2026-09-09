package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"bgl/internal/platform/logging"
	"bgl/internal/platform/requestid"
)

func WithLogging(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return requestid.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqLogger := base.With(
				"request_id", requestid.FromContext(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
			)
			ctx := logging.WithContext(r.Context(), reqLogger)

			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(sw, r.WithContext(ctx))
			elapsed := time.Since(start)

			level := slog.LevelInfo
			if sw.status >= 500 {
				level = slog.LevelError
			}
			reqLogger.Log(r.Context(), level, "request completed",
				"status", sw.status,
				"duration_ms", elapsed.Milliseconds(),
			)
		}))
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Field     string `json:"field,omitempty"`
	RequestID string `json:"request_id"`
}

func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message, field string) {
	id := requestid.FromContext(r.Context())

	logger := logging.FromContext(r.Context(), slog.Default())
	logger.Error("request error",
		"code", code,
		"message", message,
		"field", field,
		"status", status,
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorEnvelope{Error: errorBody{
		Code:      code,
		Message:   message,
		Field:     field,
		RequestID: id,
	}})
}
