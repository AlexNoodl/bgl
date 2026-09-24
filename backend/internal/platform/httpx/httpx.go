package httpx

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"bgl/internal/platform/logging"
	"bgl/internal/platform/requestid"
)

type remoteAddrCtxKey struct{}

func RemoteAddr(ctx context.Context) string {
	addr, _ := ctx.Value(remoteAddrCtxKey{}).(string)
	return addr
}

func WithLogging(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return requestid.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqLogger := base.With(
				"request_id", requestid.FromContext(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
			)
			ctx := logging.WithContext(r.Context(), reqLogger)
			ctx = context.WithValue(ctx, remoteAddrCtxKey{}, r.RemoteAddr)

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

type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Field     string `json:"field,omitempty"`
	RequestID string `json:"request_id"`
}

func LogAndBuildError(ctx context.Context, status int, code, message, field string) ErrorEnvelope {
	logger := logging.FromContext(ctx, slog.Default())
	logger.Error("request error",
		"code", code,
		"message", message,
		"field", field,
		"status", status,
	)

	return ErrorEnvelope{Error: ErrorBody{
		Code:      code,
		Message:   message,
		Field:     field,
		RequestID: requestid.FromContext(ctx),
	}}
}

func WriteError(w http.ResponseWriter, r *http.Request, status int, code, message, field string) {
	env := LogAndBuildError(r.Context(), status, code, message, field)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(env)
}

type discardStatusWriter struct {
	header http.Header
	status int
}

func (w *discardStatusWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *discardStatusWriter) Write(b []byte) (int, error) { return len(b), nil }

func (w *discardStatusWriter) WriteHeader(status int) { w.status = status }

func MuxErrors(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, pattern := mux.Handler(r)
		if pattern != "" {
			h.ServeHTTP(w, r)
			return
		}

		probe := &discardStatusWriter{}
		h.ServeHTTP(probe, r)

		if probe.status == http.StatusMethodNotAllowed {
			WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "method not allowed for this path", "")
			return
		}
		WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "no route registered yet", "")
	})
}
