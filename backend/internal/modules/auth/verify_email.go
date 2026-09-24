package auth

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"bgl/internal/platform/apperr"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

type VerifyEmailRequest struct {
	Token string `json:"token"`
}

type VerifyEmailResponse struct {
	Email      string    `json:"email"`
	VerifiedAt time.Time `json:"verified_at"`
}

type VerifyEmailDeps struct {
	Pool   *pgxpool.Pool
	Logger *slog.Logger
}

type VerifyEmailInput struct {
	Body struct {
		Token string `json:"token,omitempty"`
	}
}

type VerifyEmailOutput struct {
	Body VerifyEmailResponse
}

func RegisterVerifyEmailOperation(api huma.API, deps VerifyEmailDeps) {
	huma.Register(api, huma.Operation{
		OperationID:   "verifyEmail",
		Method:        http.MethodPost,
		Path:          "/v1/auth/verify-email",
		DefaultStatus: http.StatusOK,
		Tags:          []string{"auth"},
	}, func(ctx context.Context, input *VerifyEmailInput) (*VerifyEmailOutput, error) {
		if deps.Pool == nil {
			return nil, apperr.New(ctx, http.StatusServiceUnavailable, "NOT_READY", "DATABASE_URL is not configured", "")
		}

		if input.Body.Token == "" {
			return nil, apperr.New(ctx, http.StatusBadRequest, "VALIDATION_ERROR", "token is required", "token")
		}

		tx, err := deps.Pool.Begin(ctx)
		if err != nil {
			deps.Logger.ErrorContext(ctx, "auth: begin verify-email transaction failed", "error", err)
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process verification", "")
		}
		defer func() { _ = tx.Rollback(ctx) }()

		userID, err := consumeVerificationToken(ctx, tx, input.Body.Token, "email_verify")
		if err != nil {
			return nil, apperr.New(ctx, http.StatusBadRequest, "INVALID_OR_EXPIRED_TOKEN", "verification link is invalid or has expired", "token")
		}

		var email string
		var verifiedAt time.Time
		err = tx.QueryRow(ctx,
			`UPDATE auth.users
			   SET email_verified_at = COALESCE(email_verified_at, now())
			 WHERE id = $1
			 RETURNING email, email_verified_at`,
			userID,
		).Scan(&email, &verifiedAt)
		if err != nil {
			deps.Logger.ErrorContext(ctx, "auth: marking email verified failed", "error", err)
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process verification", "")
		}

		if err := tx.Commit(ctx); err != nil {
			deps.Logger.ErrorContext(ctx, "auth: committing verify-email transaction failed", "error", err)
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process verification", "")
		}

		return &VerifyEmailOutput{Body: VerifyEmailResponse{Email: email, VerifiedAt: verifiedAt}}, nil
	})
}
