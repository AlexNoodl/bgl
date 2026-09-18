package catalog

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func importGameMedia(ctx context.Context, tx pgx.Tx, gameID int64, coverImageID string, screenshotImageIDs []string) error {
	if _, err := tx.Exec(ctx,
		`DELETE FROM catalog.game_media WHERE game_id = $1 AND media_type IN ('cover', 'screenshot')`,
		gameID,
	); err != nil {
		return fmt.Errorf("clearing game_media: %w", err)
	}

	if coverImageID != "" {
		if _, err := tx.Exec(ctx,
			`INSERT INTO catalog.game_media (game_id, media_type, url, position) VALUES ($1, 'cover', $2, 0)`,
			gameID, igdbImageURL(coverImageID, "cover_big"),
		); err != nil {
			return fmt.Errorf("inserting cover media: %w", err)
		}
	}

	position := 0
	for _, imageID := range screenshotImageIDs {
		if imageID == "" {
			continue
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO catalog.game_media (game_id, media_type, url, position) VALUES ($1, 'screenshot', $2, $3)`,
			gameID, igdbImageURL(imageID, "screenshot_big"), position,
		); err != nil {
			return fmt.Errorf("inserting screenshot media at position %d: %w", position, err)
		}
		position++
	}

	return nil
}
