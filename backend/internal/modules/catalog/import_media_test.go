package catalog

import (
	"context"
	"os"
	"testing"

	"bgl/internal/platform/db"

	"github.com/jackc/pgx/v5"
)

func TestImportGameMedia(t *testing.T) {
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
	defer func() { _ = tx.Rollback(ctx) }()

	newGame := func(t *testing.T, sp pgx.Tx, slug string) int64 {
		t.Helper()
		var id int64
		if err := sp.QueryRow(ctx,
			`INSERT INTO catalog.games (slug, title, game_type) VALUES ($1, $1, 'main_game') RETURNING id`, slug,
		).Scan(&id); err != nil {
			t.Fatalf("inserting fixture game: %v", err)
		}
		return id
	}

	t.Run("writes cover and screenshots in order", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			gameID := newGame(t, sp, "media-test-game-1")

			if err := importGameMedia(ctx, sp, gameID, "cover-abc", []string{"shot-1", "shot-2", "shot-3"}); err != nil {
				t.Fatalf("importGameMedia: %v", err)
			}

			rows, err := sp.Query(ctx,
				`SELECT media_type, url, position FROM catalog.game_media WHERE game_id = $1 ORDER BY media_type, position`,
				gameID,
			)
			if err != nil {
				t.Fatalf("querying game_media: %v", err)
			}
			defer rows.Close()

			type row struct {
				mediaType string
				url       string
				position  int
			}
			var got []row
			for rows.Next() {
				var r row
				if err := rows.Scan(&r.mediaType, &r.url, &r.position); err != nil {
					t.Fatalf("scanning row: %v", err)
				}
				got = append(got, r)
			}

			want := []row{
				{"cover", "https://images.igdb.com/igdb/image/upload/t_cover_big/cover-abc.jpg", 0},
				{"screenshot", "https://images.igdb.com/igdb/image/upload/t_screenshot_big/shot-1.jpg", 0},
				{"screenshot", "https://images.igdb.com/igdb/image/upload/t_screenshot_big/shot-2.jpg", 1},
				{"screenshot", "https://images.igdb.com/igdb/image/upload/t_screenshot_big/shot-3.jpg", 2},
			}
			if len(got) != len(want) {
				t.Fatalf("got %d rows, want %d: %+v", len(got), len(want), got)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
				}
			}
		})
	})

	t.Run("re-importing replaces the previous set instead of appending", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			gameID := newGame(t, sp, "media-test-game-2")

			if err := importGameMedia(ctx, sp, gameID, "cover-v1", []string{"shot-v1-a", "shot-v1-b"}); err != nil {
				t.Fatalf("first importGameMedia: %v", err)
			}
			if err := importGameMedia(ctx, sp, gameID, "cover-v2", []string{"shot-v2-a"}); err != nil {
				t.Fatalf("second importGameMedia: %v", err)
			}

			var count int
			if err := sp.QueryRow(ctx, `SELECT count(*) FROM catalog.game_media WHERE game_id = $1`, gameID).Scan(&count); err != nil {
				t.Fatalf("counting game_media: %v", err)
			}
			if count != 2 {
				t.Fatalf("game_media rows = %d, want 2 (1 cover + 1 screenshot from the second import only)", count)
			}

			var coverURL string
			if err := sp.QueryRow(ctx, `SELECT url FROM catalog.game_media WHERE game_id = $1 AND media_type = 'cover'`, gameID).Scan(&coverURL); err != nil {
				t.Fatalf("reading cover: %v", err)
			}
			if coverURL != "https://images.igdb.com/igdb/image/upload/t_cover_big/cover-v2.jpg" {
				t.Errorf("cover url = %q, want the v2 cover", coverURL)
			}
		})
	})

	t.Run("no cover and no screenshots writes nothing", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			gameID := newGame(t, sp, "media-test-game-3")

			if err := importGameMedia(ctx, sp, gameID, "", nil); err != nil {
				t.Fatalf("importGameMedia: %v", err)
			}

			var count int
			if err := sp.QueryRow(ctx, `SELECT count(*) FROM catalog.game_media WHERE game_id = $1`, gameID).Scan(&count); err != nil {
				t.Fatalf("counting game_media: %v", err)
			}
			if count != 0 {
				t.Errorf("game_media rows = %d, want 0", count)
			}
		})
	})
}
