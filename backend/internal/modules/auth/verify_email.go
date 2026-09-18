package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"bgl/internal/platform/httpx"

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

func VerifyEmailHandler(deps VerifyEmailDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST", "")
			return
		}
		if deps.Pool == nil {
			httpx.WriteError(w, r, http.StatusServiceUnavailable, "NOT_READY", "DATABASE_URL is not configured", "")
			return
		}

		var req VerifyEmailRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid JSON body", "")
			return
		}
		if req.Token == "" {
			httpx.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "token is required", "token")
			return
		}

		ctx := r.Context()
		tx, err := deps.Pool.Begin(ctx)
		if err != nil {
			deps.Logger.ErrorContext(ctx, "auth: begin verify-email transaction failed", "error", err)
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process verification", "")
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()

		userID, err := consumeVerificationToken(ctx, tx, req.Token, "email_verify")
		if err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "INVALID_OR_EXPIRED_TOKEN", "verification link is invalid or has expired", "token")
			return
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
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process verification", "")
			return
		}

		if err := tx.Commit(ctx); err != nil {
			deps.Logger.ErrorContext(ctx, "auth: committing verify-email transaction failed", "error", err)
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process verification", "")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(VerifyEmailResponse{Email: email, VerifiedAt: verifiedAt})
	}
}
