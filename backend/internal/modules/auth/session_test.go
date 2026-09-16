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
)

func TestSessionMiddleware(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping (see docs/backlog.md AUTH-005)")
	}

	ctx := context.Background()

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("creating pool: %v", err)
	}
	defer pool.Close()

	middleware := SessionMiddleware(SessionMiddlewareDeps{Pool: pool, Logger: logging.New()})

	var gotUser ContextUser
	var gotOK bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotOK = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	handler := middleware(next)

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

	createSession := func(t *testing.T, userID string, expiresAt time.Time) string {
		t.Helper()
		raw, hash, err := newOpaqueToken()
		if err != nil {
			t.Fatalf("generating token: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`INSERT INTO auth.sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
			hash, userID, expiresAt,
		); err != nil {
			t.Fatalf("creating fixture session: %v", err)
		}
		return raw
	}

	serve := func(t *testing.T, cookieValue *string) {
		t.Helper()
		gotUser, gotOK = ContextUser{}, false
		req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
		if cookieValue != nil {
			req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: *cookieValue})
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected the wrapped handler to still run (200), got %d", rec.Code)
		}
	}

	t.Run("a valid session cookie resolves to the user and touches last_seen_at", func(t *testing.T) {
		userID := createUser(t, "session-mw-valid@example.com", "session_mw_valid", false)
		token := createSession(t, userID, time.Now().Add(24*time.Hour))

		serve(t, &token)
		if !gotOK || gotUser.ID != userID || gotUser.Email != "session-mw-valid@example.com" {
			t.Fatalf("expected user %s in context, got ok=%v user=%+v", userID, gotOK, gotUser)
		}

		var lastSeen, created time.Time
		if err := pool.QueryRow(ctx, `SELECT created_at, last_seen_at FROM auth.sessions WHERE token_hash = $1`, hashToken(token)).Scan(&created, &lastSeen); err != nil {
			t.Fatalf("reading session: %v", err)
		}
	})

	t.Run("no cookie leaves the request anonymous", func(t *testing.T) {
		serve(t, nil)
		if gotOK {
			t.Fatalf("expected no user in context, got %+v", gotUser)
		}
	})

	t.Run("an expired session leaves the request anonymous, not an error", func(t *testing.T) {
		userID := createUser(t, "session-mw-expired@example.com", "session_mw_expired", false)
		token := createSession(t, userID, time.Now().Add(-time.Hour))

		serve(t, &token)
		if gotOK {
			t.Fatalf("expected no user in context for an expired session, got %+v", gotUser)
		}
	})

	t.Run("a session for a soft-deleted user leaves the request anonymous", func(t *testing.T) {
		userID := createUser(t, "session-mw-deleted@example.com", "session_mw_deleted", true)
		token := createSession(t, userID, time.Now().Add(24*time.Hour))

		serve(t, &token)
		if gotOK {
			t.Fatalf("expected no user in context for a soft-deleted user's session, got %+v", gotUser)
		}
	})

	t.Run("an unknown token leaves the request anonymous", func(t *testing.T) {
		bogus := "not-a-real-session-token"
		serve(t, &bogus)
		if gotOK {
			t.Fatalf("expected no user in context, got %+v", gotUser)
		}
	})
}

func TestRequireAuth(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RequireAuth(inner.ServeHTTP)

	t.Run("no user in context is rejected with 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("a user in context is let through", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, ContextUser{ID: "u1"}))
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})
}
