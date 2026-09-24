package httpx

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteError_LogAndResponseShareRequestID(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	handler := WithLogging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, http.StatusUnprocessableEntity, "VALIDATION_ERROR", "rating must be between 1 and 10", "rating")
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/games/1/ratings", nil)
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnprocessableEntity)
	}

	var body ErrorEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding response body: %v", err)
	}
	if body.Error.Code != "VALIDATION_ERROR" {
		t.Fatalf("error.code = %q, want VALIDATION_ERROR", body.Error.Code)
	}
	if body.Error.RequestID == "" {
		t.Fatal("error.request_id is empty, want a generated ULID")
	}

	headerID := rec.Header().Get("X-Request-ID")
	if headerID != body.Error.RequestID {
		t.Fatalf("X-Request-ID header = %q, want it to match response body request_id %q", headerID, body.Error.RequestID)
	}

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, body.Error.RequestID) {
		t.Fatalf("log output does not contain request_id %q:\n%s", body.Error.RequestID, logOutput)
	}
	if !strings.Contains(logOutput, `"msg":"request error"`) {
		t.Fatalf("log output missing the WriteError log line:\n%s", logOutput)
	}
	if !strings.Contains(logOutput, `"msg":"request completed"`) {
		t.Fatalf("log output missing the WithLogging completion line:\n%s", logOutput)
	}
	if !strings.Contains(logOutput, `"level":"ERROR"`) {
		t.Fatalf("WriteError's own log line should always be at ERROR level, got:\n%s", logOutput)
	}
}

func TestWithLogging_SuccessLogsInfoNotError(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	handler := WithLogging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	handler.ServeHTTP(rec, req)

	if strings.Contains(logBuf.String(), `"level":"ERROR"`) {
		t.Fatalf("a successful request should not log at ERROR:\n%s", logBuf.String())
	}
}
