package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"bgl/internal/platform/db"
	"bgl/internal/platform/jobs"
	"bgl/internal/platform/logging"

	"github.com/danielgtaylor/huma/v2"
)

func TestForgotPasswordHandler(t *testing.T) {
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

	riverClient, err := jobs.NewInsertClient(pool)
	if err != nil {
		t.Fatalf("creating river insert client: %v", err)
	}

	handler := newTestAPI(t, func(api huma.API) {
		RegisterForgotPasswordOperation(api, ForgotPasswordDeps{Pool: pool, Jobs: riverClient, Logger: logging.New()})
	})

	createUser := func(t *testing.T, email, username string, deleted bool) string {
		t.Helper()
		var id string
		err := pool.QueryRow(ctx,
			`INSERT INTO auth.users (email, username, password_hash, deleted_at)
			 VALUES ($1, $2, 'x', CASE WHEN $3 THEN now() ELSE NULL END)
			 RETURNING id::text`,
			email, username, deleted,
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

	doForgot := func(t *testing.T, email string) *httptest.ResponseRecorder {
		t.Helper()
		payload, _ := json.Marshal(ForgotPasswordRequest{Email: email})
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/password/forgot", bytes.NewReader(payload))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	tokenCount := func(t *testing.T, userID string) int {
		t.Helper()
		var count int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM auth.verification_tokens WHERE user_id = $1 AND purpose = 'password_reset'`,
			userID,
		).Scan(&count); err != nil {
			t.Fatalf("counting tokens: %v", err)
		}
		return count
	}

	t.Run("an existing user gets a reset token queued, an unknown email gets the same response", func(t *testing.T) {
		userID := createUser(t, "forgot-handler-existing@example.com", "forgot_handler_existing", false)

		existing := doForgot(t, "forgot-handler-existing@example.com")
		unknown := doForgot(t, "forgot-handler-no-such-user@example.com")

		if existing.Code != http.StatusOK || unknown.Code != http.StatusOK {
			t.Fatalf("expected both to be 200, got %d and %d", existing.Code, unknown.Code)
		}
		if existing.Body.String() != unknown.Body.String() {
			t.Fatalf("expected identical response bodies regardless of whether the account exists, got %q and %q", existing.Body.String(), unknown.Body.String())
		}

		if got := tokenCount(t, userID); got != 1 {
			t.Fatalf("expected exactly 1 token queued, got %d", got)
		}
	})

	t.Run("a soft-deleted user's email gets the same response and no token", func(t *testing.T) {
		userID := createUser(t, "forgot-handler-deleted@example.com", "forgot_handler_deleted", true)

		rec := doForgot(t, "forgot-handler-deleted@example.com")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if got := tokenCount(t, userID); got != 0 {
			t.Fatalf("expected no token for a soft-deleted user, got %d", got)
		}
	})

	t.Run("an invalid email is a validation error", func(t *testing.T) {
		rec := doForgot(t, "not-an-email")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("a non-POST method is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/auth/password/forgot", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})
}
