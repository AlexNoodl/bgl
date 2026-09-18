package catalog

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
)

const igdbProvider = "igdb"

func ImportGame(ctx context.Context, tx pgx.Tx, g NormalizedGame) (int64, error) {
	parentGameID, err := resolveParentGameID(ctx, tx, g.ParentGameIGDBID)
	if err != nil {
		return 0, err
	}

	gameID, err := upsertGameRow(ctx, tx, g, parentGameID)
	if err != nil {
		return 0, err
	}

	if err := replaceGamePlatforms(ctx, tx, gameID, g.Platforms); err != nil {
		return 0, err
	}
	if err := replaceGameGenres(ctx, tx, gameID, g.Genres); err != nil {
		return 0, err
	}
	if err := replaceGameCompanies(ctx, tx, gameID, g.Companies); err != nil {
		return 0, err
	}

	return gameID, nil
}

func resolveParentGameID(ctx context.Context, tx pgx.Tx, parentIGDBID *int64) (*int64, error) {
	if parentIGDBID == nil {
		return nil, nil
	}
	id, found, err := lookupGameIDByExternalID(ctx, tx, strconv.FormatInt(*parentIGDBID, 10))
	if err != nil {
		return nil, fmt.Errorf("resolving parent game: %w", err)
	}
	if !found {
		return nil, nil
	}
	return &id, nil
}

func lookupGameIDByExternalID(ctx context.Context, tx pgx.Tx, providerID string) (id int64, found bool, err error) {
	err = tx.QueryRow(ctx,
		`SELECT game_id FROM catalog.external_game_ids WHERE provider = $1 AND provider_id = $2`,
		igdbProvider, providerID,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func upsertGameRow(ctx context.Context, tx pgx.Tx, g NormalizedGame, parentGameID *int64) (int64, error) {
	providerID := strconv.FormatInt(g.IGDBID, 10)

	existingID, found, err := lookupGameIDByExternalID(ctx, tx, providerID)
	if err != nil {
		return 0, fmt.Errorf("looking up existing game: %w", err)
	}

	if found {
		if _, err := tx.Exec(ctx,
			`UPDATE catalog.games
			 SET title = $1, slug = $2, summary = $3, description = $4,
			     release_date = $5, game_type = $6, parent_game_id = $7,
			     cover_image_url = $8
			 WHERE id = $9`,
			g.Title, g.Slug, nullIfEmpty(g.Summary), nullIfEmpty(g.Description),
			g.ReleaseDate, g.GameType, parentGameID, nullIfEmpty(g.CoverImageURL),
			existingID,
		); err != nil {
			return 0, fmt.Errorf("updating game: %w", err)
		}
		return existingID, nil
	}

	var gameID int64
	if err := tx.QueryRow(ctx,
		`INSERT INTO catalog.games (title, slug, summary, description, release_date, game_type, parent_game_id, cover_image_url)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 RETURNING id`,
		g.Title, g.Slug, nullIfEmpty(g.Summary), nullIfEmpty(g.Description),
		g.ReleaseDate, g.GameType, parentGameID, nullIfEmpty(g.CoverImageURL),
	).Scan(&gameID); err != nil {
		return 0, fmt.Errorf("inserting game: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO catalog.external_game_ids (game_id, provider, provider_id) VALUES ($1, $2, $3)`,
		gameID, igdbProvider, providerID,
	); err != nil {
		return 0, fmt.Errorf("recording external game id: %w", err)
	}

	return gameID, nil
}

func replaceGamePlatforms(ctx context.Context, tx pgx.Tx, gameID int64, refs []GameRef) error {
	if _, err := tx.Exec(ctx, `DELETE FROM catalog.game_platforms WHERE game_id = $1`, gameID); err != nil {
		return fmt.Errorf("clearing game_platforms: %w", err)
	}
	for _, ref := range refs {
		platformID, err := upsertBySlug(ctx, tx, "catalog.platforms", ref)
		if err != nil {
			return fmt.Errorf("upserting platform %q: %w", ref.Slug, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO catalog.game_platforms (game_id, platform_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			gameID, platformID,
		); err != nil {
			return fmt.Errorf("linking platform %q: %w", ref.Slug, err)
		}
	}
	return nil
}

func replaceGameGenres(ctx context.Context, tx pgx.Tx, gameID int64, refs []GameRef) error {
	if _, err := tx.Exec(ctx, `DELETE FROM catalog.game_genres WHERE game_id = $1`, gameID); err != nil {
		return fmt.Errorf("clearing game_genres: %w", err)
	}
	for _, ref := range refs {
		genreID, err := upsertBySlug(ctx, tx, "catalog.genres", ref)
		if err != nil {
			return fmt.Errorf("upserting genre %q: %w", ref.Slug, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO catalog.game_genres (game_id, genre_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			gameID, genreID,
		); err != nil {
			return fmt.Errorf("linking genre %q: %w", ref.Slug, err)
		}
	}
	return nil
}

func replaceGameCompanies(ctx context.Context, tx pgx.Tx, gameID int64, roles []CompanyRole) error {
	if _, err := tx.Exec(ctx, `DELETE FROM catalog.game_companies WHERE game_id = $1`, gameID); err != nil {
		return fmt.Errorf("clearing game_companies: %w", err)
	}
	for _, cr := range roles {
		companyID, err := upsertBySlug(ctx, tx, "catalog.companies", cr.Company)
		if err != nil {
			return fmt.Errorf("upserting company %q: %w", cr.Company.Slug, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO catalog.game_companies (game_id, company_id, role) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
			gameID, companyID, cr.Role,
		); err != nil {
			return fmt.Errorf("linking company %q as %s: %w", cr.Company.Slug, cr.Role, err)
		}
	}
	return nil
}

func upsertBySlug(ctx context.Context, tx pgx.Tx, table string, ref GameRef) (int, error) {
	var id int
	err := tx.QueryRow(ctx,
		fmt.Sprintf(`INSERT INTO %s (name, slug) VALUES ($1, $2)
		 ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id`, table),
		ref.Name, ref.Slug,
	).Scan(&id)
	return id, err
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
