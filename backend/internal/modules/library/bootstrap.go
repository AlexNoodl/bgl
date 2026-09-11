package library

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const DefaultWishlistName = "Wishlist"

func CreateDefaultProfile(ctx context.Context, tx pgx.Tx, userID string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO library.profiles (user_id) VALUES ($1)`, userID); err != nil {
		return fmt.Errorf("creating default profile: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO library.user_lists (user_id, name, kind) VALUES ($1, $2, 'wishlist')`,
		userID, DefaultWishlistName,
	); err != nil {
		return fmt.Errorf("creating default wishlist: %w", err)
	}

	return nil
}
