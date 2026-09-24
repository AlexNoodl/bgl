package auth

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

type LogoutDeps struct {
	Pool          *pgxpool.Pool
	Logger        *slog.Logger
	SecureCookies bool
}

type LogoutInput struct {
	// Value type, not *http.Cookie — huma panics on pointer-typed tagged
	// params. Absent cookie leaves this at its zero value (Name == "").
	SessionCookie http.Cookie `cookie:"bgl_session"`
}

type LogoutOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
}

func RegisterLogoutOperation(api huma.API, deps LogoutDeps) {
	huma.Register(api, huma.Operation{
		OperationID:   "logoutUser",
		Method:        http.MethodPost,
		Path:          "/v1/auth/logout",
		DefaultStatus: http.StatusNoContent,
		Tags:          []string{"auth"},
	}, func(ctx context.Context, input *LogoutInput) (*LogoutOutput, error) {
		if input.SessionCookie.Name != "" && deps.Pool != nil {
			if _, err := deps.Pool.Exec(ctx, `DELETE FROM auth.sessions WHERE token_hash = $1`, hashToken(input.SessionCookie.Value)); err != nil {
				deps.Logger.ErrorContext(ctx, "auth: deleting session on logout failed", "error", err)
			}
		}

		return &LogoutOutput{
			SetCookie: http.Cookie{
				Name:     SessionCookieName,
				Value:    "",
				Path:     "/",
				Expires:  time.Unix(0, 0),
				MaxAge:   -1,
				HttpOnly: true,
				Secure:   deps.SecureCookies,
				SameSite: http.SameSiteLaxMode,
			},
		}, nil
	})
}
