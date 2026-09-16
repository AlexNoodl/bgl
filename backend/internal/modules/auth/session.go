package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"bgl/internal/platform/httpx"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ContextUser struct {
	ID       string
	Email    string
	Username string
	IsAdmin  bool
}

type contextKey int

const userContextKey contextKey = iota

func UserFromContext(ctx context.Context) (ContextUser, bool) {
	u, ok := ctx.Value(userContextKey).(ContextUser)
	return u, ok
}

type SessionMiddlewareDeps struct {
	Pool   *pgxpool.Pool
	Logger *slog.Logger
}

func SessionMiddleware(deps SessionMiddlewareDeps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(SessionCookieName)
			if err != nil || deps.Pool == nil {
				next.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()
			var user ContextUser
			err = deps.Pool.QueryRow(ctx,
				`UPDATE auth.sessions s
				    SET last_seen_at = now()
				   FROM auth.users u
				  WHERE s.token_hash = $1
				    AND s.expires_at > now()
				    AND u.id = s.user_id
				    AND u.deleted_at IS NULL
				  RETURNING u.id::text, u.email, u.username, u.is_admin`,
				hashToken(cookie.Value),
			).Scan(&user.ID, &user.Email, &user.Username, &user.IsAdmin)
			if err != nil {
				if !errors.Is(err, pgx.ErrNoRows) {
					deps.Logger.ErrorContext(ctx, "auth: session lookup failed", "error", err)
				}
				next.ServeHTTP(w, r)
				return
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, userContextKey, user)))
		})
	}
}

func RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserFromContext(r.Context()); !ok {
			httpx.WriteError(w, r, http.StatusUnauthorized, "UNAUTHENTICATED", "authentication required", "")
			return
		}
		next(w, r)
	}
}
