package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"bgl/internal/modules/library"
	"bgl/internal/platform/httpx"

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

func RegisterHandler(deps RegisterDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			httpx.WriteError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST", "")
			return
		}
		if deps.Pool == nil || deps.Jobs == nil {
			httpx.WriteError(w, r, http.StatusServiceUnavailable, "NOT_READY", "DATABASE_URL is not configured", "")
			return
		}

		var req RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", "invalid JSON body", "")
			return
		}

		email, err := normalizeEmail(req.Email)
		if err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), "email")
			return
		}
		if err := validateUsername(req.Username); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), "username")
			return
		}
		if err := validatePassword(req.Password); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, "VALIDATION_ERROR", err.Error(), "password")
			return
		}

		passwordHash, err := hashPassword(req.Password)
		if err != nil {
			deps.Logger.ErrorContext(r.Context(), "auth: hashing password failed", "error", err)
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process registration", "")
			return
		}

		ctx := r.Context()
		tx, err := deps.Pool.Begin(ctx)
		if err != nil {
			deps.Logger.ErrorContext(ctx, "auth: begin registration transaction failed", "error", err)
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process registration", "")
			return
		}
		defer func() { _ = tx.Rollback(ctx) }() // no-op once committed below

		var userID string
		err = tx.QueryRow(ctx,
			`INSERT INTO auth.users (email, username, password_hash) VALUES ($1, $2, $3) RETURNING id::text`,
			email, req.Username, passwordHash,
		).Scan(&userID)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				httpx.WriteError(w, r, http.StatusConflict, "EMAIL_OR_USERNAME_ALREADY_REGISTERED", "email or username is already registered", "")
				return
			}
			deps.Logger.ErrorContext(ctx, "auth: inserting user failed", "error", err)
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process registration", "")
			return
		}

		if err := library.CreateDefaultProfile(ctx, tx, userID); err != nil {
			deps.Logger.ErrorContext(ctx, "auth: creating default profile failed", "error", err)
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process registration", "")
			return
		}

		rawToken, tokenHash, err := newOpaqueToken()
		if err != nil {
			deps.Logger.ErrorContext(ctx, "auth: generating verification token failed", "error", err)
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process registration", "")
			return
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO auth.verification_tokens (user_id, token_hash, purpose, expires_at) VALUES ($1, $2, 'email_verify', $3)`,
			userID, tokenHash, time.Now().Add(verificationTokenTTL),
		); err != nil {
			deps.Logger.ErrorContext(ctx, "auth: inserting verification token failed", "error", err)
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process registration", "")
			return
		}

		if _, err := deps.Jobs.InsertTx(ctx, tx, SendVerificationEmailArgs{
			UserID:   userID,
			Email:    email,
			Username: req.Username,
			Token:    rawToken,
		}, nil); err != nil {
			deps.Logger.ErrorContext(ctx, "auth: enqueueing verification email job failed", "error", err)
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process registration", "")
			return
		}

		if err := tx.Commit(ctx); err != nil {
			deps.Logger.ErrorContext(ctx, "auth: committing registration transaction failed", "error", err)
			httpx.WriteError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "could not process registration", "")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(RegisterResponse{
			ID:       userID,
			Email:    email,
			Username: req.Username,
		})
	}
}
