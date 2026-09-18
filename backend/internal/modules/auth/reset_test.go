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
)

func TestResetPasswordHandler(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping (see docs/backlog.md AUTH-006)")
	}

	ctx := context.Background()

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("creating pool: %v", err)
	}
	defer pool.Close()

	handler := ResetPasswordHandler(ResetPasswordDeps{Pool: pool, Logger: logging.New()})

	createUser := func(t *testing.T, email, username string) string {
		t.Helper()
		oldHash, err := hashPassword("the old password")
		if err != nil {
			t.Fatalf("hashing fixture password: %v", err)
		}
		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO auth.users (email, username, password_hash) VALUES ($1, $2, $3) RETURNING id::text`,
			email, username, oldHash,
		).Scan(&id); err != nil {
			t.Fatalf("creating fixture user: %v", err)
		}
		t.Cleanup(func() {
			if _, err := pool.Exec(ctx, `DELETE FROM auth.users WHERE id = $1`, id); err != nil {
				t.Errorf("cleaning up user %s: %v", id, err)
			}
		})
		return id
	}

	createToken := func(t *testing.T, userID string, expiresAt time.Time) string {
		t.Helper()
		raw, hash, err := newOpaqueToken()
		if err != nil {
			t.Fatalf("generating token: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO auth.verification_tokens (user_id, token_hash, purpose, expires_at) VALUES ($1, $2, 'password_reset', $3)`,
			userID, hash, expiresAt,
		); err != nil {
			t.Fatalf("creating fixture token: %v", err)
		}
		return raw
	}

	doReset := func(t *testing.T, token, newPassword string) *httptest.ResponseRecorder {
		t.Helper()
		payload, _ := json.Marshal(ResetPasswordRequest{Token: token, NewPassword: newPassword})
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/password/reset", bytes.NewReader(payload))
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}

	t.Run("a valid token changes the password and revokes existing sessions", func(t *testing.T) {
		userID := createUser(t, "reset-handler-valid@example.com", "reset_handler_valid")
		token := createToken(t, userID, time.Now().Add(time.Hour))

		if _, err := pool.Exec(ctx,
			`INSERT INTO auth.sessions (token_hash, user_id, expires_at) VALUES ('reset-test-session', $1, $2)`,
			userID, time.Now().Add(24*time.Hour),
		); err != nil {
			t.Fatalf("creating fixture session: %v", err)
		}

		rec := doReset(t, token, "a brand new password")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
		}

		var newHash string
		if err := pool.QueryRow(ctx, `SELECT password_hash FROM auth.users WHERE id = $1`, userID).Scan(&newHash); err != nil {
			t.Fatalf("reading password_hash: %v", err)
		}
		match, err := verifyPassword("a brand new password", newHash)
		if err != nil || !match {
			t.Fatalf("expected the new password to verify against the stored hash, match=%v err=%v", match, err)
		}

		var sessionCount int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM auth.sessions WHERE user_id = $1`, userID).Scan(&sessionCount); err != nil {
			t.Fatalf("counting sessions: %v", err)
		}
		if sessionCount != 0 {
			t.Fatalf("expected all sessions to be revoked, found %d", sessionCount)
		}

		second := doReset(t, token, "yet another password")
		if second.Code != http.StatusBadRequest {
			t.Fatalf("expected the token to be single-use, got %d: %s", second.Code, second.Body.String())
		}
	})

	t.Run("an expired token is rejected", func(t *testing.T) {
		userID := createUser(t, "reset-handler-expired@example.com", "reset_handler_expired")
		token := createToken(t, userID, time.Now().Add(-time.Hour))

		rec := doReset(t, token, "a brand new password")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("an email-verify token cannot be used to reset a password", func(t *testing.T) {
		userID := createUser(t, "reset-handler-wrong-purpose@example.com", "reset_handler_wrong_purpose")
		raw, hash, err := newOpaqueToken()
		if err != nil {
			t.Fatalf("generating token: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO auth.verification_tokens (user_id, token_hash, purpose, expires_at) VALUES ($1, $2, 'email_verify', $3)`,
			userID, hash, time.Now().Add(time.Hour),
		); err != nil {
			t.Fatalf("creating fixture token: %v", err)
		}

		rec := doReset(t, raw, "a brand new password")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("a weak new password is a validation error and does not consume the token", func(t *testing.T) {
		userID := createUser(t, "reset-handler-weak@example.com", "reset_handler_weak")
		token := createToken(t, userID, time.Now().Add(time.Hour))

		rec := doReset(t, token, "short")
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
		if envelope.Error.Code != "VALIDATION_ERROR" || envelope.Error.Field != "new_password" {
			t.Fatalf("unexpected error envelope: %+v", envelope.Error)
		}

		// the token must still be usable — validation happens before consuming it.
		ok := doReset(t, token, "a perfectly fine password")
		if ok.Code != http.StatusNoContent {
			t.Fatalf("expected the token to still be valid after a rejected weak password, got %d: %s", ok.Code, ok.Body.String())
		}
	})

	t.Run("a non-POST method is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/auth/password/reset", nil)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})
}
