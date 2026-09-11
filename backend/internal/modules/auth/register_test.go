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
)

func TestRegisterHandler(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping (see docs/backlog.md AUTH-002)")
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

	deps := RegisterDeps{
		Pool:   pool,
		Jobs:   riverClient,
		Logger: logging.New(),
	}
	handler := RegisterHandler(deps)

	cleanupUser := func(t *testing.T, email string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `DELETE FROM auth.users WHERE email = $1`, email); err != nil {
			t.Errorf("cleaning up user %s: %v", email, err)
		}
	}

	doRegister := func(t *testing.T, body RegisterRequest) *httptest.ResponseRecorder {
		t.Helper()
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshaling request: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/register", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}

	t.Run("a valid registration creates exactly one user, profile, wishlist, and token", func(t *testing.T) {
		const email = "register-handler-test@example.com"
		t.Cleanup(func() { cleanupUser(t, email) })

		rec := doRegister(t, RegisterRequest{Email: email, Username: "register_handler_test", Password: "correct horse battery"})
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp RegisterResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
		if resp.Email != email || resp.Username != "register_handler_test" || resp.ID == "" {
			t.Fatalf("unexpected response body: %+v", resp)
		}

		var users, profiles, wishlists, tokens int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM auth.users WHERE id = $1`, resp.ID).Scan(&users); err != nil {
			t.Fatalf("counting users: %v", err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM library.profiles WHERE user_id = $1`, resp.ID).Scan(&profiles); err != nil {
			t.Fatalf("counting profiles: %v", err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM library.user_lists WHERE user_id = $1 AND kind = 'wishlist'`, resp.ID).Scan(&wishlists); err != nil {
			t.Fatalf("counting wishlists: %v", err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM auth.verification_tokens WHERE user_id = $1 AND purpose = 'email_verify'`, resp.ID).Scan(&tokens); err != nil {
			t.Fatalf("counting tokens: %v", err)
		}
		if users != 1 || profiles != 1 || wishlists != 1 || tokens != 1 {
			t.Fatalf("expected exactly one row in each table, got users=%d profiles=%d wishlists=%d tokens=%d", users, profiles, wishlists, tokens)
		}
	})

	t.Run("registering with an already-registered email is rejected without creating a second user", func(t *testing.T) {
		const email = "register-handler-dup-email-test@example.com"
		t.Cleanup(func() { cleanupUser(t, email) })

		first := doRegister(t, RegisterRequest{Email: email, Username: "register_handler_dup_email_1", Password: "correct horse battery"})
		if first.Code != http.StatusCreated {
			t.Fatalf("expected first registration to succeed, got %d: %s", first.Code, first.Body.String())
		}

		second := doRegister(t, RegisterRequest{Email: email, Username: "register_handler_dup_email_2", Password: "correct horse battery"})
		if second.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d: %s", second.Code, second.Body.String())
		}

		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM auth.users WHERE email = $1`, email).Scan(&count); err != nil {
			t.Fatalf("counting users: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected exactly one user row for %s, got %d", email, count)
		}
	})

	t.Run("registering with an already-registered username (any case) is rejected", func(t *testing.T) {
		const email1 = "register-handler-dup-username-1@example.com"
		const email2 = "register-handler-dup-username-2@example.com"
		t.Cleanup(func() { cleanupUser(t, email1) })
		t.Cleanup(func() { cleanupUser(t, email2) })

		first := doRegister(t, RegisterRequest{Email: email1, Username: "Register_Handler_Dup_User", Password: "correct horse battery"})
		if first.Code != http.StatusCreated {
			t.Fatalf("expected first registration to succeed, got %d: %s", first.Code, first.Body.String())
		}

		second := doRegister(t, RegisterRequest{Email: email2, Username: "register_handler_dup_user", Password: "correct horse battery"})
		if second.Code != http.StatusConflict {
			t.Fatalf("expected 409 for a case-insensitive username collision, got %d: %s", second.Code, second.Body.String())
		}
	})

	t.Run("validation errors are rejected before touching the database", func(t *testing.T) {
		cases := []struct {
			name  string
			body  RegisterRequest
			field string
		}{
			{"bad email", RegisterRequest{Email: "not-an-email", Username: "valid_username", Password: "correct horse battery"}, "email"},
			{"short username", RegisterRequest{Email: "validation-test@example.com", Username: "ab", Password: "correct horse battery"}, "username"},
			{"short password", RegisterRequest{Email: "validation-test@example.com", Username: "valid_username", Password: "short"}, "password"},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				rec := doRegister(t, c.body)
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
				if envelope.Error.Code != "VALIDATION_ERROR" {
					t.Fatalf("expected code VALIDATION_ERROR, got %q", envelope.Error.Code)
				}
				if envelope.Error.Field != c.field {
					t.Fatalf("expected field %q, got %q", c.field, envelope.Error.Field)
				}
			})
		}
	})

	t.Run("a non-POST method is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/auth/register", nil)
		rec := httptest.NewRecorder()
		handler(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})
}
