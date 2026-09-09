package auth

import (
	"context"
	"errors"
	"os"
	"testing"

	"bgl/internal/platform/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestAuthSchemaConstraints(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping (see docs/backlog.md AUTH-001)")
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

	const email = "schema-test-1@example.com"

	var firstID string
	err = tx.QueryRow(ctx,
		`INSERT INTO auth.users (email, username) VALUES ($1, $2) RETURNING id::text`,
		email, "schema_test_user",
	).Scan(&firstID)
	if err != nil {
		t.Fatalf("inserting first user: %v", err)
	}

	t.Run("duplicate email is rejected", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			_, err := sp.Exec(ctx,
				`INSERT INTO auth.users (email, username) VALUES ($1, $2)`,
				email, "someone_else",
			)
			assertUniqueViolation(t, err)
		})
	})

	t.Run("duplicate username is rejected regardless of case", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			_, err := sp.Exec(ctx,
				`INSERT INTO auth.users (email, username) VALUES ($1, $2)`,
				"schema-test-2@example.com", "SCHEMA_TEST_USER",
			)
			assertUniqueViolation(t, err)
		})
	})

	t.Run("email is reusable after the owning account is soft-deleted", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			if _, err := sp.Exec(ctx, `UPDATE auth.users SET deleted_at = now() WHERE id = $1`, firstID); err != nil {
				t.Fatalf("soft-deleting the first user: %v", err)
			}
			if _, err := sp.Exec(ctx,
				`INSERT INTO auth.users (email, username) VALUES ($1, $2)`,
				email, "schema_test_user_2",
			); err != nil {
				t.Fatalf("re-registering the same email after soft delete should succeed, got: %v", err)
			}
		})
	})

	t.Run("duplicate oauth provider identity is rejected", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			_, err := sp.Exec(ctx,
				`INSERT INTO auth.oauth_accounts (user_id, provider, provider_user_id) VALUES ($1, 'google', 'g-123')`,
				firstID,
			)
			if err != nil {
				t.Fatalf("inserting first oauth account: %v", err)
			}

			withSavepoint(t, ctx, sp, func(sp2 pgx.Tx) {
				_, err := sp2.Exec(ctx,
					`INSERT INTO auth.oauth_accounts (user_id, provider, provider_user_id) VALUES ($1, 'google', 'g-123')`,
					firstID,
				)
				assertUniqueViolation(t, err)
			})
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
