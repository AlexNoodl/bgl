package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"bgl/internal/platform/httpx"

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

func ForgotPasswordHandler(deps ForgotPasswordDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST", "")
			return
		}
		if deps.Pool == nil || deps.Jobs == nil {
			httpx.WriteError(w, r, http.StatusServiceUnavailable, "NOT_READY", "DATABASE_URL is not configured", "")
			return
		}

		var req ForgotPasswordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid JSON body", "")
			return
		}
		email, err := normalizeEmail(req.Email)
		if err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), "email")
			return
		}

		ctx := r.Context()

		defer func() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"message": "if an account exists for that email, a password reset link has been sent",
			})
		}()

		var userID, username string
		err = deps.Pool.QueryRow(ctx,
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
}
