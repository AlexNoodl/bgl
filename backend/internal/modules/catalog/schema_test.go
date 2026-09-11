package catalog

import (
	"context"
	"errors"
	"os"
	"testing"

	"bgl/internal/platform/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestCatalogSchemaConstraints(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping (see docs/backlog.md GAME-001)")
	}

	ctx := context.Background()

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("creating pool: %v", err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // always rolled back, never commits real rows

	var franchiseID, seriesID, platformID, genreID, companyID int64
	err = tx.QueryRow(ctx, `INSERT INTO catalog.franchises (name, slug) VALUES ('Test Franchise', 'schema-test-franchise') RETURNING id`).Scan(&franchiseID)
	if err != nil {
		t.Fatalf("inserting franchise: %v", err)
	}
	err = tx.QueryRow(ctx, `INSERT INTO catalog.series (name, slug, franchise_id) VALUES ('Test Series', 'schema-test-series', $1) RETURNING id`, franchiseID).Scan(&seriesID)
	if err != nil {
		t.Fatalf("inserting series: %v", err)
	}
	err = tx.QueryRow(ctx, `INSERT INTO catalog.platforms (name, slug) VALUES ('Test Platform', 'schema-test-platform') RETURNING id`).Scan(&platformID)
	if err != nil {
		t.Fatalf("inserting platform: %v", err)
	}
	err = tx.QueryRow(ctx, `INSERT INTO catalog.genres (name, slug) VALUES ('Test Genre', 'schema-test-genre') RETURNING id`).Scan(&genreID)
	if err != nil {
		t.Fatalf("inserting genre: %v", err)
	}
	err = tx.QueryRow(ctx, `INSERT INTO catalog.companies (name, slug) VALUES ('Test Company', 'schema-test-company') RETURNING id`).Scan(&companyID)
	if err != nil {
		t.Fatalf("inserting company: %v", err)
	}

	var gameID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO catalog.games (slug, title, game_type, franchise_id, series_id) VALUES ($1, $2, 'main_game', $3, $4) RETURNING id`,
		"schema-test-game", "Schema Test Game", franchiseID, seriesID,
	).Scan(&gameID)
	if err != nil {
		t.Fatalf("inserting game: %v", err)
	}

	t.Run("duplicate slug is rejected", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			_, err := sp.Exec(ctx,
				`INSERT INTO catalog.games (slug, title, game_type) VALUES ($1, 'Other Title', 'main_game')`,
				"schema-test-game",
			)
			assertUniqueViolation(t, err)
		})
	})

	t.Run("franchise_id/series_id reject unknown ids (deferred FK added after catalog.franchises/series)", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			_, err := sp.Exec(ctx,
				`INSERT INTO catalog.games (slug, title, game_type, franchise_id) VALUES ('schema-test-bad-franchise', 'Bad', 'main_game', 999999)`,
			)
			assertForeignKeyViolation(t, err)
		})
	})

	t.Run("game_companies allows the same company as both developer and publisher", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			if _, err := sp.Exec(ctx,
				`INSERT INTO catalog.game_companies (game_id, company_id, role) VALUES ($1, $2, 'developer')`,
				gameID, companyID,
			); err != nil {
				t.Fatalf("inserting developer role: %v", err)
			}
			if _, err := sp.Exec(ctx,
				`INSERT INTO catalog.game_companies (game_id, company_id, role) VALUES ($1, $2, 'publisher')`,
				gameID, companyID,
			); err != nil {
				t.Fatalf("inserting publisher role for the same game/company should succeed, got: %v", err)
			}

			withSavepoint(t, ctx, sp, func(sp2 pgx.Tx) {
				_, err := sp2.Exec(ctx,
					`INSERT INTO catalog.game_companies (game_id, company_id, role) VALUES ($1, $2, 'developer')`,
					gameID, companyID,
				)
				assertUniqueViolation(t, err)
			})
		})
	})

	t.Run("external_game_ids rejects duplicate provider+provider_id", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			if _, err := sp.Exec(ctx,
				`INSERT INTO catalog.external_game_ids (game_id, provider, provider_id) VALUES ($1, 'igdb', 'igdb-schema-test')`,
				gameID,
			); err != nil {
				t.Fatalf("inserting first external id: %v", err)
			}

			withSavepoint(t, ctx, sp, func(sp2 pgx.Tx) {
				_, err := sp2.Exec(ctx,
					`INSERT INTO catalog.external_game_ids (game_id, provider, provider_id) VALUES ($1, 'igdb', 'igdb-schema-test')`,
					gameID,
				)
				assertUniqueViolation(t, err)
			})
		})
	})

	t.Run("deleting a game cascades to its join-table rows", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			var childGameID int64
			if err := sp.QueryRow(ctx,
				`INSERT INTO catalog.games (slug, title, game_type) VALUES ('schema-test-cascade-game', 'Cascade Game', 'main_game') RETURNING id`,
			).Scan(&childGameID); err != nil {
				t.Fatalf("inserting cascade-test game: %v", err)
			}
			if _, err := sp.Exec(ctx, `INSERT INTO catalog.game_platforms (game_id, platform_id) VALUES ($1, $2)`, childGameID, platformID); err != nil {
				t.Fatalf("inserting game_platforms row: %v", err)
			}
			if _, err := sp.Exec(ctx, `INSERT INTO catalog.game_genres (game_id, genre_id) VALUES ($1, $2)`, childGameID, genreID); err != nil {
				t.Fatalf("inserting game_genres row: %v", err)
			}
			if _, err := sp.Exec(ctx, `INSERT INTO catalog.game_media (game_id, media_type, url) VALUES ($1, 'cover', 'https://example.com/cover.png')`, childGameID); err != nil {
				t.Fatalf("inserting game_media row: %v", err)
			}

			if _, err := sp.Exec(ctx, `DELETE FROM catalog.games WHERE id = $1`, childGameID); err != nil {
				t.Fatalf("deleting game: %v", err)
			}

			var platforms, genres, media int
			if err := sp.QueryRow(ctx, `SELECT count(*) FROM catalog.game_platforms WHERE game_id = $1`, childGameID).Scan(&platforms); err != nil {
				t.Fatalf("counting game_platforms: %v", err)
			}
			if err := sp.QueryRow(ctx, `SELECT count(*) FROM catalog.game_genres WHERE game_id = $1`, childGameID).Scan(&genres); err != nil {
				t.Fatalf("counting game_genres: %v", err)
			}
			if err := sp.QueryRow(ctx, `SELECT count(*) FROM catalog.game_media WHERE game_id = $1`, childGameID).Scan(&media); err != nil {
				t.Fatalf("counting game_media: %v", err)
			}
			if platforms != 0 || genres != 0 || media != 0 {
				t.Fatalf("expected cascade delete to remove all join rows, got platforms=%d genres=%d media=%d", platforms, genres, media)
			}
		})
	})

	t.Run("updated_at trigger fires on UPDATE", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			if _, err := sp.Exec(ctx, `UPDATE catalog.games SET title = 'Schema Test Game (updated)' WHERE id = $1`, gameID); err != nil {
				t.Fatalf("updating game: %v", err)
			}
			var updatedAtMatchesNow bool
			if err := sp.QueryRow(ctx, `SELECT updated_at = now() FROM catalog.games WHERE id = $1`, gameID).Scan(&updatedAtMatchesNow); err != nil {
				t.Fatalf("reading after state: %v", err)
			}
			if !updatedAtMatchesNow {
				t.Fatal("expected catalog_games_set_updated_at trigger to set updated_at to now() on UPDATE")
			}
		})
	})
}

func withSavepoint(t *testing.T, ctx context.Context, parent pgx.Tx, fn func(pgx.Tx)) {
	t.Helper()

	sp, err := parent.Begin(ctx)
	if err != nil {
		t.Fatalf("begin savepoint: %v", err)
	}
	defer func() { _ = sp.Rollback(ctx) }()

	fn(sp)
}

func assertUniqueViolation(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("expected a unique_violation (23505) error, got none")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("expected a unique_violation (23505) error, got: %v", err)
	}
}

func assertForeignKeyViolation(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("expected a foreign_key_violation (23503) error, got none")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Fatalf("expected a foreign_key_violation (23503) error, got: %v", err)
	}
}
