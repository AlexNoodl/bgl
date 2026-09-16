package auth

import (
	"log/slog"
	"net/http"
	"time"

	"bgl/internal/platform/httpx"

	"github.com/jackc/pgx/v5/pgxpool"
)

type LogoutDeps struct {
	Pool          *pgxpool.Pool
	Logger        *slog.Logger
	SecureCookies bool
}

func LogoutHandler(deps LogoutDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST", "")
			return
		}

		if cookie, err := r.Cookie(SessionCookieName); err == nil && deps.Pool != nil {
			if _, err := deps.Pool.Exec(r.Context(), `DELETE FROM auth.sessions WHERE token_hash = $1`, hashToken(cookie.Value)); err != nil {
				deps.Logger.ErrorContext(r.Context(), "auth: deleting session on logout failed", "error", err)
			}
		}

		http.SetCookie(w, &http.Cookie{
			Name:     SessionCookieName,
			Value:    "",
			Path:     "/",
			Expires:  time.Unix(0, 0),
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   deps.SecureCookies,
			SameSite: http.SameSiteLaxMode,
		})

		w.WriteHeader(http.StatusNoContent)
	}
}
