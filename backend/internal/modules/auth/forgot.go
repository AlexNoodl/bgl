package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"bgl/internal/platform/apperr"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

const passwordResetTokenTTL = 1 * time.Hour

type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

type ForgotPasswordDeps struct {
	Pool   *pgxpool.Pool
	Jobs   *river.Client[pgx.Tx]
	Logger *slog.Logger
}

type ForgotPasswordInput struct {
	Body struct {
		Email string `json:"email,omitempty"`
	}
}

type ForgotPasswordOutput struct {
	Body struct {
		Message string `json:"message"`
	}
}

const forgotPasswordUniformMessage = "if an account exists for that email, a password reset link has been sent"

func RegisterForgotPasswordOperation(api huma.API, deps ForgotPasswordDeps) {
	huma.Register(api, huma.Operation{
		OperationID:   "forgotPassword",
		Method:        http.MethodPost,
		Path:          "/v1/auth/password/forgot",
		DefaultStatus: http.StatusOK,
		Tags:          []string{"auth"},
	}, func(ctx context.Context, input *ForgotPasswordInput) (*ForgotPasswordOutput, error) {
		if deps.Pool == nil || deps.Jobs == nil {
			return nil, apperr.New(ctx, http.StatusServiceUnavailable, "NOT_READY", "DATABASE_URL is not configured", "")
		}

		email, err := normalizeEmail(input.Body.Email)
		if err != nil {
			return nil, apperr.New(ctx, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), "email")
		}

		attemptSendResetEmail(ctx, deps, email)

		out := &ForgotPasswordOutput{}
		out.Body.Message = forgotPasswordUniformMessage
		return out, nil
	})
}

func attemptSendResetEmail(ctx context.Context, deps ForgotPasswordDeps, email string) {
	var userID, username string
	err := deps.Pool.QueryRow(ctx,
		`SELECT id::text, username FROM auth.users WHERE email = $1 AND deleted_at IS NULL`,
		email,
	).Scan(&userID, &username)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			deps.Logger.ErrorContext(ctx, "auth: looking up user for password reset failed", "error", err)
		}
		return
	}

	rawToken, tokenHash, err := newOpaqueToken()
	if err != nil {
		deps.Logger.ErrorContext(ctx, "auth: generating password reset token failed", "error", err)
		return
	}

	tx, err := deps.Pool.Begin(ctx)
	if err != nil {
		deps.Logger.ErrorContext(ctx, "auth: begin password reset request transaction failed", "error", err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`INSERT INTO auth.verification_tokens (user_id, token_hash, purpose, expires_at) VALUES ($1, $2, 'password_reset', $3)`,
		userID, tokenHash, time.Now().Add(passwordResetTokenTTL),
	); err != nil {
		deps.Logger.ErrorContext(ctx, "auth: inserting password reset token failed", "error", err)
		return
	}

	if _, err := deps.Jobs.InsertTx(ctx, tx, SendPasswordResetEmailArgs{
		UserID:   userID,
		Email:    email,
		Username: username,
		Token:    rawToken,
	}, nil); err != nil {
		deps.Logger.ErrorContext(ctx, "auth: enqueueing password reset email job failed", "error", err)
		return
	}

	if err := tx.Commit(ctx); err != nil {
		deps.Logger.ErrorContext(ctx, "auth: committing password reset request transaction failed", "error", err)
	}
}
