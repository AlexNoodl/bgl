package library

import (
	"context"
	"errors"
	"os"
	"testing"

	"bgl/internal/platform/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestListsSchemaConstraints(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping (see docs/backlog.md LIST-001)")
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

	var userID string
	err = tx.QueryRow(ctx,
		`INSERT INTO auth.users (email, username) VALUES ($1, $2) RETURNING id::text`,
		"list-schema-test@example.com", "list_schema_test_user",
	).Scan(&userID)
	if err != nil {
		t.Fatalf("inserting user: %v", err)
	}

	var gameID int64
	err = tx.QueryRow(ctx,
		`INSERT INTO catalog.games (slug, title, game_type) VALUES ('list-schema-test-game', 'List Schema Test Game', 'main_game') RETURNING id`,
	).Scan(&gameID)
	if err != nil {
		t.Fatalf("inserting game: %v", err)
	}

	var wishlistID string
	err = tx.QueryRow(ctx,
		`INSERT INTO library.user_lists (user_id, name, kind) VALUES ($1, 'My Wishlist', 'wishlist') RETURNING id::text`,
		userID,
	).Scan(&wishlistID)
	if err != nil {
		t.Fatalf("inserting wishlist: %v", err)
	}

	t.Run("a second wishlist for the same user is rejected", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			_, err := sp.Exec(ctx,
				`INSERT INTO library.user_lists (user_id, name, kind) VALUES ($1, 'Another Wishlist', 'wishlist')`,
				userID,
			)
			assertUniqueViolation(t, err)
		})
	})

	t.Run("multiple custom lists for the same user are allowed", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			if _, err := sp.Exec(ctx, `INSERT INTO library.user_lists (user_id, name) VALUES ($1, 'Custom List 1')`, userID); err != nil {
				t.Fatalf("inserting first custom list: %v", err)
			}
			if _, err := sp.Exec(ctx, `INSERT INTO library.user_lists (user_id, name) VALUES ($1, 'Custom List 2')`, userID); err != nil {
				t.Fatalf("inserting second custom list should succeed, got: %v", err)
			}
		})
	})

	t.Run("kind and visibility default to custom/public", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			var listID string
			if err := sp.QueryRow(ctx, `INSERT INTO library.user_lists (user_id, name) VALUES ($1, 'Defaults List') RETURNING id::text`, userID).Scan(&listID); err != nil {
				t.Fatalf("inserting list: %v", err)
			}
			var kind, visibility string
			if err := sp.QueryRow(ctx, `SELECT kind, visibility FROM library.user_lists WHERE id = $1`, listID).Scan(&kind, &visibility); err != nil {
				t.Fatalf("reading defaults: %v", err)
			}
			if kind != "custom" || visibility != "public" {
				t.Fatalf("expected kind=custom visibility=public, got kind=%s visibility=%s", kind, visibility)
			}
		})
	})

	t.Run("an unknown kind is rejected", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			_, err := sp.Exec(ctx, `INSERT INTO library.user_lists (user_id, name, kind) VALUES ($1, 'Bad Kind', 'favorites')`, userID)
			assertCheckViolation(t, err)
		})
	})

	t.Run("an unknown visibility is rejected", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			_, err := sp.Exec(ctx, `INSERT INTO library.user_lists (user_id, name, visibility) VALUES ($1, 'Bad Vis', 'friends-only')`, userID)
			assertCheckViolation(t, err)
		})
	})

	t.Run("a game appears at most once per list", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			if _, err := sp.Exec(ctx, `INSERT INTO library.user_list_items (list_id, game_id, position) VALUES ($1, $2, 1)`, wishlistID, gameID); err != nil {
				t.Fatalf("inserting first item: %v", err)
			}
			withSavepoint(t, ctx, sp, func(sp2 pgx.Tx) {
				_, err := sp2.Exec(ctx, `INSERT INTO library.user_list_items (list_id, game_id, position) VALUES ($1, $2, 2)`, wishlistID, gameID)
				assertUniqueViolation(t, err)
			})
		})
	})

	t.Run("a game referenced by a list item cannot be hard-deleted (RESTRICT)", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			var restrictedGameID int64
			if err := sp.QueryRow(ctx,
				`INSERT INTO catalog.games (slug, title, game_type) VALUES ('list-schema-test-restrict-game', 'Restrict Game', 'main_game') RETURNING id`,
			).Scan(&restrictedGameID); err != nil {
				t.Fatalf("inserting game: %v", err)
			}
			if _, err := sp.Exec(ctx, `INSERT INTO library.user_list_items (list_id, game_id, position) VALUES ($1, $2, 1)`, wishlistID, restrictedGameID); err != nil {
				t.Fatalf("inserting item: %v", err)
			}

			if _, err := sp.Exec(ctx, `UPDATE catalog.games SET deleted_at = now() WHERE id = $1`, restrictedGameID); err != nil {
				t.Fatalf("soft-deleting the game should succeed, got: %v", err)
			}

			withSavepoint(t, ctx, sp, func(sp2 pgx.Tx) {
				_, err := sp2.Exec(ctx, `DELETE FROM catalog.games WHERE id = $1`, restrictedGameID)
				assertForeignKeyViolation(t, err)
			})
		})
	})

	t.Run("deleting the owning user cascades through lists to list items", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			var cascadeUserID string
			if err := sp.QueryRow(ctx,
				`INSERT INTO auth.users (email, username) VALUES ('list-schema-test-cascade@example.com', 'list_schema_test_cascade_user') RETURNING id::text`,
			).Scan(&cascadeUserID); err != nil {
				t.Fatalf("inserting cascade-test user: %v", err)
			}
			var cascadeListID string
			if err := sp.QueryRow(ctx,
				`INSERT INTO library.user_lists (user_id, name) VALUES ($1, 'Cascade List') RETURNING id::text`,
				cascadeUserID,
			).Scan(&cascadeListID); err != nil {
				t.Fatalf("inserting cascade-test list: %v", err)
			}
			if _, err := sp.Exec(ctx, `INSERT INTO library.user_list_items (list_id, game_id, position) VALUES ($1, $2, 1)`, cascadeListID, gameID); err != nil {
				t.Fatalf("inserting cascade-test item: %v", err)
			}

			if _, err := sp.Exec(ctx, `DELETE FROM auth.users WHERE id = $1`, cascadeUserID); err != nil {
				t.Fatalf("deleting user: %v", err)
			}

			var listsLeft, itemsLeft int
			if err := sp.QueryRow(ctx, `SELECT count(*) FROM library.user_lists WHERE id = $1`, cascadeListID).Scan(&listsLeft); err != nil {
				t.Fatalf("counting lists: %v", err)
			}
			if err := sp.QueryRow(ctx, `SELECT count(*) FROM library.user_list_items WHERE list_id = $1`, cascadeListID).Scan(&itemsLeft); err != nil {
				t.Fatalf("counting items: %v", err)
			}
			if listsLeft != 0 || itemsLeft != 0 {
				t.Fatalf("expected cascade delete to remove the list and its items, got lists=%d items=%d", listsLeft, itemsLeft)
			}
		})
	})

	t.Run("updated_at trigger fires on UPDATE", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			if _, err := sp.Exec(ctx, `UPDATE library.user_lists SET name = 'My Wishlist (updated)' WHERE id = $1`, wishlistID); err != nil {
				t.Fatalf("updating list: %v", err)
			}
			var updatedAtMatchesNow bool
			if err := sp.QueryRow(ctx, `SELECT updated_at = now() FROM library.user_lists WHERE id = $1`, wishlistID).Scan(&updatedAtMatchesNow); err != nil {
				t.Fatalf("reading after state: %v", err)
			}
			if !updatedAtMatchesNow {
				t.Fatal("expected library_user_lists_set_updated_at trigger to set updated_at to now() on UPDATE")
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

func assertCheckViolation(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("expected a check_violation (23514) error, got none")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("expected a check_violation (23514) error, got: %v", err)
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
