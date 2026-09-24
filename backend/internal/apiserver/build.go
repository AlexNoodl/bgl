package apiserver

import (
	"log/slog"
	"net/http"

	"bgl/internal/modules/auth"
	"bgl/internal/platform/apperr"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

type Deps struct {
	Pool          *pgxpool.Pool
	RiverClient   *river.Client[pgx.Tx]
	Logger        *slog.Logger
	SecureCookies bool
}

func BuildAPI(mux *http.ServeMux, deps Deps) huma.API {
	apperr.OverrideHumaErrors()

	config := huma.DefaultConfig("BGL API", "1.0.0")
	config.CreateHooks = nil

	api := humago.New(mux, config)

	auth.RegisterRegisterOperation(api, auth.RegisterDeps{
		Pool:   deps.Pool,
		Jobs:   deps.RiverClient,
		Logger: deps.Logger,
	})
	auth.RegisterLoginOperation(api, auth.LoginDeps{
		Pool:          deps.Pool,
		Logger:        deps.Logger,
		SecureCookies: deps.SecureCookies,
	})
	auth.RegisterLogoutOperation(api, auth.LogoutDeps{
		Pool:          deps.Pool,
		Logger:        deps.Logger,
		SecureCookies: deps.SecureCookies,
	})
	auth.RegisterForgotPasswordOperation(api, auth.ForgotPasswordDeps{
		Pool:   deps.Pool,
		Jobs:   deps.RiverClient,
		Logger: deps.Logger,
	})
	auth.RegisterResetPasswordOperation(api, auth.ResetPasswordDeps{
		Pool:   deps.Pool,
		Logger: deps.Logger,
	})
	auth.RegisterVerifyEmailOperation(api, auth.VerifyEmailDeps{
		Pool:   deps.Pool,
		Logger: deps.Logger,
	})

	return api
}
