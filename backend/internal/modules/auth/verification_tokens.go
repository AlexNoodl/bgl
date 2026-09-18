package auth

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func consumeVerificationToken(ctx context.Context, tx pgx.Tx, rawToken, purpose string) (userID string, err error) {
	err = tx.QueryRow(ctx,
		`UPDATE auth.verification_tokens
		   SET used_at = now()
		 WHERE token_hash = $1 AND purpose = $2 AND used_at IS NULL AND expires_at > now()
		 RETURNING user_id::text`,
		hashToken(rawToken), purpose,
	).Scan(&userID)
	return userID, err
}
