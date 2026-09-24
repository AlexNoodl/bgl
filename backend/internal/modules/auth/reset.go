package auth

import (
	"context"
	"log/slog"
	"net/http"

	"bgl/internal/platform/apperr"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ResetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

type ResetPasswordDeps struct {
	Pool   *pgxpool.Pool
	Logger *slog.Logger
}

type ResetPasswordInput struct {
	Body struct {
		Token       string `json:"token,omitempty"`
		NewPassword string `json:"new_password,omitempty"`
	}
}

type ResetPasswordOutput struct{}

func RegisterResetPasswordOperation(api huma.API, deps ResetPasswordDeps) {
	huma.Register(api, huma.Operation{
		OperationID:   "resetPassword",
		Method:        http.MethodPost,
		Path:          "/v1/auth/password/reset",
		DefaultStatus: http.StatusNoContent,
		Tags:          []string{"auth"},
	}, func(ctx context.Context, input *ResetPasswordInput) (*ResetPasswordOutput, error) {
		if deps.Pool == nil {
			return nil, apperr.New(ctx, http.StatusServiceUnavailable, "NOT_READY", "DATABASE_URL is not configured", "")
		}

		if input.Body.Token == "" {
			return nil, apperr.New(ctx, http.StatusBadRequest, "VALIDATION_ERROR", "token is required", "token")
		}
		if err := validatePassword(input.Body.NewPassword); err != nil {
			return nil, apperr.New(ctx, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), "new_password")
		}

		tx, err := deps.Pool.Begin(ctx)
		if err != nil {
			deps.Logger.ErrorContext(ctx, "auth: begin password reset transaction failed", "error", err)
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process password reset", "")
		}
		defer func() { _ = tx.Rollback(ctx) }()

		// Validate the new password (above) before consuming the token — the
		// token must stay usable if only the password was rejected.
		userID, err := consumeVerificationToken(ctx, tx, input.Body.Token, "password_reset")
		if err != nil {
			return nil, apperr.New(ctx, http.StatusBadRequest, "INVALID_OR_EXPIRED_TOKEN", "reset link is invalid or has expired", "token")
		}

		newHash, err := hashPassword(input.Body.NewPassword)
		if err != nil {
			deps.Logger.ErrorContext(ctx, "auth: hashing new password failed", "error", err)
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process password reset", "")
		}

		if _, err := tx.Exec(ctx, `UPDATE auth.users SET password_hash = $1 WHERE id = $2`, newHash, userID); err != nil {
			deps.Logger.ErrorContext(ctx, "auth: updating password failed", "error", err)
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process password reset", "")
		}

		if _, err := tx.Exec(ctx, `DELETE FROM auth.sessions WHERE user_id = $1`, userID); err != nil {
			deps.Logger.ErrorContext(ctx, "auth: revoking sessions after password reset failed", "error", err)
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process password reset", "")
		}

		if err := tx.Commit(ctx); err != nil {
			deps.Logger.ErrorContext(ctx, "auth: committing password reset transaction failed", "error", err)
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process password reset", "")
		}

		return &ResetPasswordOutput{}, nil
	})
}
