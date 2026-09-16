package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"bgl/internal/platform/httpx"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	SessionCookieName = "bgl_session"
	sessionTTL        = 30 * 24 * time.Hour
)

var dummyPasswordHash = mustHashPassword("bgl-login-dummy-password-for-timing-safety")

func mustHashPassword(password string) string {
	hash, err := hashPassword(password)
	if err != nil {
		panic(err)
	}
	return hash
}

type LoginRequest struct {
	Identifier string `json:"identifier"`
	Password   string `json:"password"`
}

type LoginResponse struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Username string `json:"username"`
}

type LoginDeps struct {
	Pool          *pgxpool.Pool
	Logger        *slog.Logger
	SecureCookies bool
}

func LoginHandler(deps LoginDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST", "")
			return
		}
		if deps.Pool == nil {
			httpx.WriteError(w, r, http.StatusServiceUnavailable, "NOT_READY", "DATABASE_URL is not configured", "")
			return
		}

		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid JSON body", "")
			return
		}
		identifier := strings.TrimSpace(req.Identifier)
		if identifier == "" {
			httpx.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "identifier is required", "identifier")
			return
		}
		if req.Password == "" {
			httpx.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "password is required", "password")
			return
		}

		ctx := r.Context()

		var userID, email, username string
		var passwordHash *string
		err := deps.Pool.QueryRow(ctx,
			`SELECT id::text, email, username, password_hash
			   FROM auth.users
			  WHERE (email = $1 OR lower(username) = lower($1)) AND deleted_at IS NULL`,
			identifier,
		).Scan(&userID, &email, &username, &passwordHash)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			deps.Logger.ErrorContext(ctx, "auth: looking up user for login failed", "error", err)
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process login", "")
			return
		}

		found := err == nil && passwordHash != nil
		hashToCompare := dummyPasswordHash
		if found {
			hashToCompare = *passwordHash
		}

		match, err := verifyPassword(req.Password, hashToCompare)
		if err != nil {
			deps.Logger.ErrorContext(ctx, "auth: verifying password failed", "error", err)
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process login", "")
			return
		}
		if !found || !match {
			httpx.WriteError(w, r, http.StatusUnauthorized, "INVALID_CREDENTIALS", "identifier or password is incorrect", "")
			return
		}

		rawToken, tokenHash, err := newOpaqueToken()
		if err != nil {
			deps.Logger.ErrorContext(ctx, "auth: generating session token failed", "error", err)
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process login", "")
			return
		}
		expiresAt := time.Now().Add(sessionTTL)

		var userAgent any
		if ua := r.UserAgent(); ua != "" {
			userAgent = ua
		}

		if _, err := deps.Pool.Exec(ctx,
			`INSERT INTO auth.sessions (token_hash, user_id, user_agent, ip_hash, expires_at) VALUES ($1, $2, $3, $4, $5)`,
			tokenHash, userID, userAgent, hashToken(clientIP(r)), expiresAt,
		); err != nil {
			deps.Logger.ErrorContext(ctx, "auth: creating session failed", "error", err)
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process login", "")
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     SessionCookieName,
			Value:    rawToken,
			Path:     "/",
			Expires:  expiresAt,
			HttpOnly: true,
			Secure:   deps.SecureCookies,
			SameSite: http.SameSiteLaxMode,
		})

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(LoginResponse{ID: userID, Email: email, Username: username})
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
