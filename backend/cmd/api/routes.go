package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"bgl/internal/modules/auth"
	"bgl/internal/platform/db"
	"bgl/internal/platform/httpx"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

type routesDeps struct {
	Pool          *pgxpool.Pool
	RiverClient   *river.Client[pgx.Tx]
	Logger        *slog.Logger
	SecureCookies bool
}

func registerRoutes(mux *http.ServeMux, deps routesDeps) {
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if deps.Pool == nil {
			httpx.WriteError(w, r, http.StatusServiceUnavailable, "NOT_READY", "DATABASE_URL is not configured", "")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx, deps.Pool); err != nil {
			httpx.WriteError(w, r, http.StatusServiceUnavailable, "NOT_READY", "database unavailable", "")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("/v1/auth/register", auth.RegisterHandler(auth.RegisterDeps{
		Pool:   deps.Pool,
		Jobs:   deps.RiverClient,
		Logger: deps.Logger,
	}))
	mux.HandleFunc("/v1/auth/verify-email", auth.VerifyEmailHandler(auth.VerifyEmailDeps{
		Pool:   deps.Pool,
		Logger: deps.Logger,
	}))
	mux.HandleFunc("/v1/auth/login", auth.LoginHandler(auth.LoginDeps{
		Pool:          deps.Pool,
		Logger:        deps.Logger,
		SecureCookies: deps.SecureCookies,
	}))
	mux.HandleFunc("/v1/auth/logout", auth.LogoutHandler(auth.LogoutDeps{
		Pool:          deps.Pool,
		Logger:        deps.Logger,
		SecureCookies: deps.SecureCookies,
	}))
	mux.HandleFunc("/v1/auth/password/forgot", auth.ForgotPasswordHandler(auth.ForgotPasswordDeps{
		Pool:   deps.Pool,
		Jobs:   deps.RiverClient,
		Logger: deps.Logger,
	}))
	mux.HandleFunc("/v1/auth/password/reset", auth.ResetPasswordHandler(auth.ResetPasswordDeps{
		Pool:   deps.Pool,
		Logger: deps.Logger,
	}))

	// Temporary manual-verification route for AUTH-005 — exercises
	// auth.SessionMiddleware/UserFromContext before GET /v1/me (PROFILE-002)
	// exists. Delete once that lands.
	mux.HandleFunc("/internal/debug/whoami", func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok {
			httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "no active session", "")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":       user.ID,
			"email":    user.Email,
			"username": user.Username,
			"is_admin": user.IsAdmin,
		})
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, http.StatusNotFound, "NOT_FOUND", "no route registered yet", "")
	})
}
