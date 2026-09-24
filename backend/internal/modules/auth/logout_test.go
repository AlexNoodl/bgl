package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"bgl/internal/platform/db"
	"bgl/internal/platform/logging"

	"github.com/danielgtaylor/huma/v2"
)

func TestLogoutHandler(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping")
	}

	ctx := context.Background()

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("creating pool: %v", err)
	}
	t.Cleanup(pool.Close)

	handler := newTestAPI(t, func(api huma.API) {
		RegisterLogoutOperation(api, LogoutDeps{Pool: pool, Logger: logging.New(), SecureCookies: false})
	})

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO auth.users (email, username, password_hash) VALUES ('logout-handler-test@example.com', 'logout_handler_test', 'x') RETURNING id::text`,
	).Scan(&userID); err != nil {
		t.Fatalf("creating fixture user: %v", err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM auth.users WHERE id = $1`, userID); err != nil {
			t.Errorf("cleaning up user: %v", err)
		}
	})

	raw, hash, err := newOpaqueToken()
	if err != nil {
		t.Fatalf("generating token: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO auth.sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		hash, userID, time.Now().Add(24*time.Hour),
	); err != nil {
		t.Fatalf("creating fixture session: %v", err)
	}

	t.Run("logging out deletes the session and clears the cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
		req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: raw})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
		}

		cookies := rec.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != SessionCookieName || cookies[0].MaxAge >= 0 {
			t.Fatalf("expected a clearing cookie, got %+v", cookies)
		}

		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM auth.sessions WHERE token_hash = $1`, hash).Scan(&count); err != nil {
			t.Fatalf("counting sessions: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected the session row to be gone, found %d", count)
		}
	})

	t.Run("logging out without a session cookie still succeeds", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("a non-POST method is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/auth/logout", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})
}
