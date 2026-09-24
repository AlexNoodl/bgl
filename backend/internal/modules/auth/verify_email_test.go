package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"bgl/internal/platform/db"
	"bgl/internal/platform/logging"

	"github.com/danielgtaylor/huma/v2"
)

func TestVerifyEmailHandler(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping (see docs/backlog.md AUTH-003)")
	}

	ctx := context.Background()

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("creating pool: %v", err)
	}
	defer pool.Close()

	handler := newTestAPI(t, func(api huma.API) {
		RegisterVerifyEmailOperation(api, VerifyEmailDeps{Pool: pool, Logger: logging.New()})
	})

	createUser := func(t *testing.T, email, username string) string {
		t.Helper()
		var id string
		err := pool.QueryRow(ctx,
			`INSERT INTO auth.users (email, username, password_hash) VALUES ($1, $2, 'x') RETURNING id::text`,
			email, username,
		).Scan(&id)
		if err != nil {
			t.Fatalf("creating fixture user: %v", err)
		}
		t.Cleanup(func() {
			if _, err := pool.Exec(ctx, `DELETE FROM auth.users WHERE id = $1`, id); err != nil {
				t.Errorf("cleaning up user %s: %v", id, err)
			}
		})
		return id
	}

	createToken := func(t *testing.T, userID, purpose string, expiresAt time.Time, used bool) string {
		t.Helper()
		raw, hash, err := newOpaqueToken()
		if err != nil {
			t.Fatalf("generating token: %v", err)
		}
		usedAt := any(nil)
		if used {
			usedAt = time.Now().Add(-time.Minute)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO auth.verification_tokens (user_id, token_hash, purpose, expires_at, used_at) VALUES ($1, $2, $3, $4, $5)`,
			userID, hash, purpose, expiresAt, usedAt,
		); err != nil {
			t.Fatalf("creating fixture token: %v", err)
		}
		return raw
	}

	doVerify := func(t *testing.T, token string) *httptest.ResponseRecorder {
		t.Helper()
		payload, _ := json.Marshal(VerifyEmailRequest{Token: token})
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/verify-email", bytes.NewReader(payload))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	t.Run("a valid token verifies the user and cannot be reused", func(t *testing.T) {
		userID := createUser(t, "verify-handler-valid@example.com", "verify_handler_valid")
		token := createToken(t, userID, "email_verify", time.Now().Add(24*time.Hour), false)

		rec := doVerify(t, token)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp VerifyEmailResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
		if resp.Email != "verify-handler-valid@example.com" {
			t.Fatalf("unexpected response body: %+v", resp)
		}

		var verifiedAt any
		if err := pool.QueryRow(ctx, `SELECT email_verified_at FROM auth.users WHERE id = $1`, userID).Scan(&verifiedAt); err != nil {
			t.Fatalf("reading email_verified_at: %v", err)
		}
		if verifiedAt == nil {
			t.Fatalf("expected email_verified_at to be set")
		}

		second := doVerify(t, token)
		if second.Code != http.StatusBadRequest {
			t.Fatalf("expected replay to be rejected with 400, got %d: %s", second.Code, second.Body.String())
		}
	})

	t.Run("an expired token is rejected", func(t *testing.T) {
		userID := createUser(t, "verify-handler-expired@example.com", "verify_handler_expired")
		token := createToken(t, userID, "email_verify", time.Now().Add(-time.Hour), false)

		rec := doVerify(t, token)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("an already-used token is rejected", func(t *testing.T) {
		userID := createUser(t, "verify-handler-used@example.com", "verify_handler_used")
		token := createToken(t, userID, "email_verify", time.Now().Add(24*time.Hour), true)

		rec := doVerify(t, token)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("a token issued for a different purpose is rejected", func(t *testing.T) {
		userID := createUser(t, "verify-handler-wrong-purpose@example.com", "verify_handler_wrong_purpose")
		token := createToken(t, userID, "password_reset", time.Now().Add(24*time.Hour), false)

		rec := doVerify(t, token)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("an unknown token is rejected", func(t *testing.T) {
		rec := doVerify(t, "not-a-real-token")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("an empty token is a validation error", func(t *testing.T) {
		rec := doVerify(t, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
		var envelope struct {
			Error struct {
				Code  string `json:"code"`
				Field string `json:"field"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("decoding error envelope: %v", err)
		}
		if envelope.Error.Code != "VALIDATION_ERROR" || envelope.Error.Field != "token" {
			t.Fatalf("unexpected error envelope: %+v", envelope.Error)
		}
	})

	t.Run("a non-POST method is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/auth/verify-email", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})
}
