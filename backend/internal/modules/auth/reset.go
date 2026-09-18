package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"bgl/internal/platform/httpx"

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

func ResetPasswordHandler(deps ResetPasswordDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST", "")
			return
		}
		if deps.Pool == nil {
			httpx.WriteError(w, r, http.StatusServiceUnavailable, "NOT_READY", "DATABASE_URL is not configured", "")
			return
		}

		var req ResetPasswordRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid JSON body", "")
			return
		}
		if req.Token == "" {
			httpx.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "token is required", "token")
			return
		}
		if err := validatePassword(req.NewPassword); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), "new_password")
			return
		}

		ctx := r.Context()
		tx, err := deps.Pool.Begin(ctx)
		if err != nil {
			deps.Logger.ErrorContext(ctx, "auth: begin password reset transaction failed", "error", err)
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process password reset", "")
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()

		userID, err := consumeVerificationToken(ctx, tx, req.Token, "password_reset")
		if err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_OR_EXPIRED_TOKEN", "reset link is invalid or has expired", "token")
			return
		}

		newHash, err := hashPassword(req.NewPassword)
		if err != nil {
			deps.Logger.ErrorContext(ctx, "auth: hashing new password failed", "error", err)
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process password reset", "")
			return
		}

		if _, err := tx.Exec(ctx, `UPDATE auth.users SET password_hash = $1 WHERE id = $2`, newHash, userID); err != nil {
			deps.Logger.ErrorContext(ctx, "auth: updating password failed", "error", err)
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process password reset", "")
			return
		}

		if _, err := tx.Exec(ctx, `DELETE FROM auth.sessions WHERE user_id = $1`, userID); err != nil {
			deps.Logger.ErrorContext(ctx, "auth: revoking sessions after password reset failed", "error", err)
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process password reset", "")
			return
		}

		if err := tx.Commit(ctx); err != nil {
			deps.Logger.ErrorContext(ctx, "auth: committing password reset transaction failed", "error", err)
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process password reset", "")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
