package auth

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"bgl/internal/platform/apperr"
	"bgl/internal/platform/httpx"

	"github.com/danielgtaylor/huma/v2"
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

type LoginInput struct {
	UserAgent string `header:"User-Agent"`
	Body      struct {
		Identifier string `json:"identifier,omitempty"`
		Password   string `json:"password,omitempty"`
	}
}

type LoginOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      LoginResponse
}

func RegisterLoginOperation(api huma.API, deps LoginDeps) {
	huma.Register(api, huma.Operation{
		OperationID:   "loginUser",
		Method:        http.MethodPost,
		Path:          "/v1/auth/login",
		DefaultStatus: http.StatusOK,
		Tags:          []string{"auth"},
	}, func(ctx context.Context, input *LoginInput) (*LoginOutput, error) {
		if deps.Pool == nil {
			return nil, apperr.New(ctx, http.StatusServiceUnavailable, "NOT_READY", "DATABASE_URL is not configured", "")
		}

		identifier := strings.TrimSpace(input.Body.Identifier)
		if identifier == "" {
			return nil, apperr.New(ctx, http.StatusBadRequest, "VALIDATION_ERROR", "identifier is required", "identifier")
		}
		if input.Body.Password == "" {
			return nil, apperr.New(ctx, http.StatusBadRequest, "VALIDATION_ERROR", "password is required", "password")
		}

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
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process login", "")
		}

		found := err == nil && passwordHash != nil
		hashToCompare := dummyPasswordHash
		if found {
			hashToCompare = *passwordHash
		}

		match, err := verifyPassword(input.Body.Password, hashToCompare)
		if err != nil {
			deps.Logger.ErrorContext(ctx, "auth: verifying password failed", "error", err)
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process login", "")
		}
		if !found || !match {
			return nil, apperr.New(ctx, http.StatusUnauthorized, "INVALID_CREDENTIALS", "identifier or password is incorrect", "")
		}

		rawToken, tokenHash, err := newOpaqueToken()
		if err != nil {
			deps.Logger.ErrorContext(ctx, "auth: generating session token failed", "error", err)
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process login", "")
		}
		expiresAt := time.Now().Add(sessionTTL)

		var userAgent any
		if input.UserAgent != "" {
			userAgent = input.UserAgent
		}

		if _, err := deps.Pool.Exec(ctx,
			`INSERT INTO auth.sessions (token_hash, user_id, user_agent, ip_hash, expires_at) VALUES ($1, $2, $3, $4, $5)`,
			tokenHash, userID, userAgent, hashToken(clientIP(httpx.RemoteAddr(ctx))), expiresAt,
		); err != nil {
			deps.Logger.ErrorContext(ctx, "auth: creating session failed", "error", err)
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process login", "")
		}

		return &LoginOutput{
			SetCookie: http.Cookie{
				Name:     SessionCookieName,
				Value:    rawToken,
				Path:     "/",
				Expires:  expiresAt,
				HttpOnly: true,
				Secure:   deps.SecureCookies,
				SameSite: http.SameSiteLaxMode,
			},
			Body: LoginResponse{ID: userID, Email: email, Username: username},
		}, nil
	})
}

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
