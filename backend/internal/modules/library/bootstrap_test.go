package library

import (
	"context"
	"os"
	"testing"

	"bgl/internal/platform/db"

	"github.com/jackc/pgx/v5"
)

func TestProfilesSchemaConstraints(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping (see docs/backlog.md PROFILE-001)")
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
	defer func() { _ = tx.Rollback(ctx) }()

	var userID string
	err = tx.QueryRow(ctx,
		`INSERT INTO auth.users (email, username) VALUES ($1, $2) RETURNING id::text`,
		"profile-schema-test@example.com", "profile_schema_test_user",
	).Scan(&userID)
	if err != nil {
		t.Fatalf("inserting user: %v", err)
	}

	t.Run("is_public/notes_public/ratings_public default to true", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			if _, err := sp.Exec(ctx, `INSERT INTO library.profiles (user_id) VALUES ($1)`, userID); err != nil {
				t.Fatalf("inserting profile: %v", err)
			}
			var isPublic, notesPublic, ratingsPublic bool
			if err := sp.QueryRow(ctx,
				`SELECT is_public, notes_public, ratings_public FROM library.profiles WHERE user_id = $1`,
				userID,
			).Scan(&isPublic, &notesPublic, &ratingsPublic); err != nil {
				t.Fatalf("reading defaults: %v", err)
			}
			if !isPublic || !notesPublic || !ratingsPublic {
				t.Fatalf("expected all three flags to default to true, got is_public=%v notes_public=%v ratings_public=%v", isPublic, notesPublic, ratingsPublic)
			}
		})
	})

	t.Run("a second profile for the same user is rejected", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			if _, err := sp.Exec(ctx, `INSERT INTO library.profiles (user_id) VALUES ($1)`, userID); err != nil {
				t.Fatalf("inserting first profile: %v", err)
			}
			withSavepoint(t, ctx, sp, func(sp2 pgx.Tx) {
				_, err := sp2.Exec(ctx, `INSERT INTO library.profiles (user_id) VALUES ($1)`, userID)
				assertUniqueViolation(t, err)
			})
		})
	})

	t.Run("deleting the user cascades to the profile", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			var cascadeUserID string
			if err := sp.QueryRow(ctx,
				`INSERT INTO auth.users (email, username) VALUES ('profile-schema-test-cascade@example.com', 'profile_schema_test_cascade_user') RETURNING id::text`,
			).Scan(&cascadeUserID); err != nil {
				t.Fatalf("inserting cascade-test user: %v", err)
			}
			if _, err := sp.Exec(ctx, `INSERT INTO library.profiles (user_id) VALUES ($1)`, cascadeUserID); err != nil {
				t.Fatalf("inserting cascade-test profile: %v", err)
			}
			if _, err := sp.Exec(ctx, `DELETE FROM auth.users WHERE id = $1`, cascadeUserID); err != nil {
				t.Fatalf("deleting user: %v", err)
			}
			var profilesLeft int
			if err := sp.QueryRow(ctx, `SELECT count(*) FROM library.profiles WHERE user_id = $1`, cascadeUserID).Scan(&profilesLeft); err != nil {
				t.Fatalf("counting profiles: %v", err)
			}
			if profilesLeft != 0 {
				t.Fatalf("expected cascade delete to remove the profile, got %d", profilesLeft)
			}
		})
	})

	t.Run("updated_at trigger fires on UPDATE", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			if _, err := sp.Exec(ctx, `INSERT INTO library.profiles (user_id) VALUES ($1)`, userID); err != nil {
				t.Fatalf("inserting profile: %v", err)
			}
			if _, err := sp.Exec(ctx, `UPDATE library.profiles SET bio = 'updated bio' WHERE user_id = $1`, userID); err != nil {
				t.Fatalf("updating profile: %v", err)
			}
			var updatedAtMatchesNow bool
			if err := sp.QueryRow(ctx, `SELECT updated_at = now() FROM library.profiles WHERE user_id = $1`, userID).Scan(&updatedAtMatchesNow); err != nil {
				t.Fatalf("reading after state: %v", err)
			}
			if !updatedAtMatchesNow {
				t.Fatal("expected library_profiles_set_updated_at trigger to set updated_at to now() on UPDATE")
			}
		})
	})
}

func TestCreateDefaultProfile(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping (see docs/backlog.md PROFILE-001)")
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
	defer func() { _ = tx.Rollback(ctx) }()

	var userID string
	err = tx.QueryRow(ctx,
		`INSERT INTO auth.users (email, username) VALUES ($1, $2) RETURNING id::text`,
		"bootstrap-func-test@example.com", "bootstrap_func_test_user",
	).Scan(&userID)
	if err != nil {
		t.Fatalf("inserting user: %v", err)
	}

	if err := CreateDefaultProfile(ctx, tx, userID); err != nil {
		t.Fatalf("CreateDefaultProfile: %v", err)
	}

	var profileCount, wishlistCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM library.profiles WHERE user_id = $1`, userID).Scan(&profileCount); err != nil {
		t.Fatalf("counting profiles: %v", err)
	}
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM library.user_lists WHERE user_id = $1 AND kind = 'wishlist'`, userID,
	).Scan(&wishlistCount); err != nil {
		t.Fatalf("counting wishlists: %v", err)
	}
	if profileCount != 1 || wishlistCount != 1 {
		t.Fatalf("expected exactly one profile and one wishlist, got profiles=%d wishlists=%d", profileCount, wishlistCount)
	}

	if err := CreateDefaultProfile(ctx, tx, userID); err == nil {
		t.Fatal("expected a second CreateDefaultProfile call for the same user to fail, got nil error")
	}
}
