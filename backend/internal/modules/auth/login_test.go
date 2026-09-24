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
	"bgl/internal/platform/logging"

	"github.com/danielgtaylor/huma/v2"
)

func TestLoginHandler(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping")
	}

	ctx := context.Background()

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("creating pool: %v", err)
	}
	defer pool.Close()

	handler := newTestAPI(t, func(api huma.API) {
		RegisterLoginOperation(api, LoginDeps{Pool: pool, Logger: logging.New(), SecureCookies: false})
	})

	createUser := func(t *testing.T, email, username, password string, deleted bool) string {
		t.Helper()
		var passwordHash *string
		if password != "" {
			h, err := hashPassword(password)
			if err != nil {
				t.Fatalf("hashing fixture password: %v", err)
			}
			passwordHash = &h
		}
		var id string
		err := pool.QueryRow(ctx,
			`INSERT INTO auth.users (email, username, password_hash, deleted_at)
			 VALUES ($1, $2, $3, CASE WHEN $4 THEN now() ELSE NULL END)
			 RETURNING id::text`,
			email, username, passwordHash, deleted,
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

	doLogin := func(t *testing.T, identifier, password string) *httptest.ResponseRecorder {
		t.Helper()
		payload, _ := json.Marshal(LoginRequest{Identifier: identifier, Password: password})
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(payload))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	t.Run("a valid login by email succeeds and sets a session cookie", func(t *testing.T) {
		userID := createUser(t, "login-handler-email@example.com", "login_handler_email", "correct horse battery", false)

		rec := doLogin(t, "login-handler-email@example.com", "correct horse battery")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp LoginResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decoding response: %v", err)
		}
		if resp.ID != userID {
			t.Fatalf("unexpected response body: %+v", resp)
		}

		cookies := rec.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != SessionCookieName || cookies[0].Value == "" {
			t.Fatalf("expected a session cookie, got %+v", cookies)
		}

		var sessionCount int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM auth.sessions WHERE user_id = $1`, userID).Scan(&sessionCount); err != nil {
			t.Fatalf("counting sessions: %v", err)
		}
		if sessionCount != 1 {
			t.Fatalf("expected exactly one session row, got %d", sessionCount)
		}
	})

	t.Run("a valid login by username in a different case succeeds", func(t *testing.T) {
		createUser(t, "login-handler-username@example.com", "Login_Handler_User", "correct horse battery", false)

		rec := doLogin(t, "login_handler_user", "correct horse battery")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("a wrong password is rejected with the same code as an unknown identifier", func(t *testing.T) {
		createUser(t, "login-handler-wrong-pw@example.com", "login_handler_wrong_pw", "correct horse battery", false)

		wrongPassword := doLogin(t, "login-handler-wrong-pw@example.com", "not the password")
		unknown := doLogin(t, "no-such-user@example.com", "whatever")

		if wrongPassword.Code != http.StatusUnauthorized || unknown.Code != http.StatusUnauthorized {
			t.Fatalf("expected both to be 401, got %d and %d", wrongPassword.Code, unknown.Code)
		}

		var wrongPasswordBody, unknownBody struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(wrongPassword.Body.Bytes(), &wrongPasswordBody)
		_ = json.Unmarshal(unknown.Body.Bytes(), &unknownBody)
		if wrongPasswordBody.Error.Code != "INVALID_CREDENTIALS" || unknownBody.Error.Code != wrongPasswordBody.Error.Code {
			t.Fatalf("expected both to carry code INVALID_CREDENTIALS, got %q and %q", wrongPasswordBody.Error.Code, unknownBody.Error.Code)
		}
	})

	t.Run("a soft-deleted user cannot log in", func(t *testing.T) {
		createUser(t, "login-handler-deleted@example.com", "login_handler_deleted", "correct horse battery", true)

		rec := doLogin(t, "login-handler-deleted@example.com", "correct horse battery")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("an OAuth-only user (no password set) cannot log in with a password", func(t *testing.T) {
		createUser(t, "login-handler-oauth-only@example.com", "login_handler_oauth_only", "", false)

		rec := doLogin(t, "login-handler-oauth-only@example.com", "anything")
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("missing identifier or password is a validation error", func(t *testing.T) {
		cases := []struct {
			name  string
			body  LoginRequest
			field string
		}{
			{"missing identifier", LoginRequest{Password: "something"}, "identifier"},
			{"missing password", LoginRequest{Identifier: "someone@example.com"}, "password"},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				rec := doLogin(t, c.body.Identifier, c.body.Password)
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
				if envelope.Error.Code != "VALIDATION_ERROR" || envelope.Error.Field != c.field {
					t.Fatalf("unexpected error envelope: %+v", envelope.Error)
				}
			})
		}
	})

	t.Run("a non-POST method is rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/auth/login", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})
}
