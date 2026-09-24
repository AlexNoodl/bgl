package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"bgl/internal/modules/library"
	"bgl/internal/platform/apperr"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

const verificationTokenTTL = 24 * time.Hour

type RegisterRequest struct {
	Email    string `json:"email"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type RegisterResponse struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	Username string `json:"username"`
}

type RegisterDeps struct {
	Pool   *pgxpool.Pool
	Jobs   *river.Client[pgx.Tx]
	Logger *slog.Logger
}

type RegisterInput struct {
	Body struct {
		Email    string `json:"email,omitempty"`
		Username string `json:"username,omitempty"`
		Password string `json:"password,omitempty"`
	}
}

type RegisterOutput struct {
	Body RegisterResponse
}

func RegisterRegisterOperation(api huma.API, deps RegisterDeps) {
	huma.Register(api, huma.Operation{
		OperationID:   "registerUser",
		Method:        http.MethodPost,
		Path:          "/v1/auth/register",
		DefaultStatus: http.StatusCreated,
		Tags:          []string{"auth"},
	}, func(ctx context.Context, input *RegisterInput) (*RegisterOutput, error) {
		if deps.Pool == nil || deps.Jobs == nil {
			return nil, apperr.New(ctx, http.StatusServiceUnavailable, "NOT_READY", "DATABASE_URL is not configured", "")
		}

		email, err := normalizeEmail(input.Body.Email)
		if err != nil {
			return nil, apperr.New(ctx, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), "email")
		}
		if err := validateUsername(input.Body.Username); err != nil {
			return nil, apperr.New(ctx, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), "username")
		}
		if err := validatePassword(input.Body.Password); err != nil {
			return nil, apperr.New(ctx, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), "password")
		}

		passwordHash, err := hashPassword(input.Body.Password)
		if err != nil {
			deps.Logger.ErrorContext(ctx, "auth: hashing password failed", "error", err)
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process registration", "")
		}

		tx, err := deps.Pool.Begin(ctx)
		if err != nil {
			deps.Logger.ErrorContext(ctx, "auth: begin registration transaction failed", "error", err)
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process registration", "")
		}
		defer func() { _ = tx.Rollback(ctx) }() // no-op once committed below

		var userID string
		err = tx.QueryRow(ctx,
			`INSERT INTO auth.users (email, username, password_hash) VALUES ($1, $2, $3) RETURNING id::text`,
			email, input.Body.Username, passwordHash,
		).Scan(&userID)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return nil, apperr.New(ctx, http.StatusConflict, "EMAIL_OR_USERNAME_ALREADY_REGISTERED", "email or username is already registered", "")
			}
			deps.Logger.ErrorContext(ctx, "auth: inserting user failed", "error", err)
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process registration", "")
		}

		if err := library.CreateDefaultProfile(ctx, tx, userID); err != nil {
			deps.Logger.ErrorContext(ctx, "auth: creating default profile failed", "error", err)
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process registration", "")
		}

		rawToken, tokenHash, err := newOpaqueToken()
		if err != nil {
			deps.Logger.ErrorContext(ctx, "auth: generating verification token failed", "error", err)
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process registration", "")
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO auth.verification_tokens (user_id, token_hash, purpose, expires_at) VALUES ($1, $2, 'email_verify', $3)`,
			userID, tokenHash, time.Now().Add(verificationTokenTTL),
		); err != nil {
			deps.Logger.ErrorContext(ctx, "auth: inserting verification token failed", "error", err)
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process registration", "")
		}

		if _, err := deps.Jobs.InsertTx(ctx, tx, SendVerificationEmailArgs{
			UserID:   userID,
			Email:    email,
			Username: input.Body.Username,
			Token:    rawToken,
		}, nil); err != nil {
			deps.Logger.ErrorContext(ctx, "auth: enqueueing verification email job failed", "error", err)
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process registration", "")
		}

		if err := tx.Commit(ctx); err != nil {
			deps.Logger.ErrorContext(ctx, "auth: committing registration transaction failed", "error", err)
			return nil, apperr.New(ctx, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process registration", "")
		}

		return &RegisterOutput{Body: RegisterResponse{
			ID:       userID,
			Email:    email,
			Username: input.Body.Username,
		}}, nil
	})
}
