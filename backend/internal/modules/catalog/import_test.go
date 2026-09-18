package catalog

import (
	"context"
	"os"
	"testing"
	"time"

	"bgl/internal/platform/db"

	"github.com/jackc/pgx/v5"
)

func TestImportGame(t *testing.T) {
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

	countRows := func(t *testing.T, sp pgx.Tx, query string, args ...any) int {
		t.Helper()
		var n int
		if err := sp.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatalf("counting rows (%s): %v", query, err)
		}
		return n
	}

	t.Run("inserts a new game with platforms, genres, and companies", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			g := NormalizedGame{
				IGDBID:   9001,
				Title:    "Import Test Game",
				Slug:     "import-test-game-9001",
				GameType: "main_game",
				Platforms: []GameRef{
					{IGDBID: 6, Name: "PC (Microsoft Windows)", Slug: "import-test-pc"},
				},
				Genres: []GameRef{
					{IGDBID: 12, Name: "Role-playing (RPG)", Slug: "import-test-rpg"},
				},
				Companies: []CompanyRole{
					{Company: GameRef{IGDBID: 1012, Name: "FromSoftware", Slug: "import-test-fromsoftware"}, Role: "developer"},
					{Company: GameRef{IGDBID: 1012, Name: "FromSoftware", Slug: "import-test-fromsoftware"}, Role: "publisher"},
				},
			}

			gameID, err := ImportGame(ctx, sp, g)
			if err != nil {
				t.Fatalf("ImportGame: %v", err)
			}

			var title, gameType string
			if err := sp.QueryRow(ctx, `SELECT title, game_type FROM catalog.games WHERE id = $1`, gameID).Scan(&title, &gameType); err != nil {
				t.Fatalf("reading imported game: %v", err)
			}
			if title != "Import Test Game" || gameType != "main_game" {
				t.Errorf("title/game_type = %q/%q, want %q/%q", title, gameType, "Import Test Game", "main_game")
			}

			if n := countRows(t, sp, `SELECT count(*) FROM catalog.external_game_ids WHERE game_id = $1 AND provider = 'igdb' AND provider_id = '9001'`, gameID); n != 1 {
				t.Errorf("external_game_ids rows = %d, want 1", n)
			}
			if n := countRows(t, sp, `SELECT count(*) FROM catalog.game_platforms WHERE game_id = $1`, gameID); n != 1 {
				t.Errorf("game_platforms rows = %d, want 1", n)
			}
			if n := countRows(t, sp, `SELECT count(*) FROM catalog.game_genres WHERE game_id = $1`, gameID); n != 1 {
				t.Errorf("game_genres rows = %d, want 1", n)
			}
			if n := countRows(t, sp, `SELECT count(*) FROM catalog.game_companies WHERE game_id = $1`, gameID); n != 2 {
				t.Errorf("game_companies rows = %d, want 2 (developer + publisher)", n)
			}
		})
	})

	t.Run("re-importing the same IGDB game updates fields and join rows instead of duplicating", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			firstDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			first := NormalizedGame{
				IGDBID:      9002,
				Title:       "Reimport Test Game",
				Slug:        "reimport-test-game-9002",
				GameType:    "main_game",
				ReleaseDate: &firstDate,
				Platforms: []GameRef{
					{IGDBID: 6, Name: "PC (Microsoft Windows)", Slug: "reimport-test-pc"},
				},
			}
			firstID, err := ImportGame(ctx, sp, first)
			if err != nil {
				t.Fatalf("first ImportGame: %v", err)
			}

			correctedDate := time.Date(2020, 3, 15, 0, 0, 0, 0, time.UTC)
			second := NormalizedGame{
				IGDBID:      9002,
				Title:       "Reimport Test Game (Corrected)",
				Slug:        "reimport-test-game-9002",
				GameType:    "main_game",
				ReleaseDate: &correctedDate,
				Platforms: []GameRef{
					{IGDBID: 48, Name: "PlayStation 4", Slug: "reimport-test-ps4"},
				},
			}
			secondID, err := ImportGame(ctx, sp, second)
			if err != nil {
				t.Fatalf("second ImportGame: %v", err)
			}

			if firstID != secondID {
				t.Fatalf("second import got a different game id (%d vs %d) — expected the same row to be updated", secondID, firstID)
			}
			if n := countRows(t, sp, `SELECT count(*) FROM catalog.games WHERE id = $1`, firstID); n != 1 {
				t.Fatalf("games rows for this id = %d, want 1", n)
			}
			if n := countRows(t, sp, `SELECT count(*) FROM catalog.external_game_ids WHERE provider = 'igdb' AND provider_id = '9002'`); n != 1 {
				t.Errorf("external_game_ids rows for igdb/9002 = %d, want 1 (no duplicate)", n)
			}

			var title string
			var releaseDate time.Time
			if err := sp.QueryRow(ctx, `SELECT title, release_date FROM catalog.games WHERE id = $1`, firstID).Scan(&title, &releaseDate); err != nil {
				t.Fatalf("reading updated game: %v", err)
			}
			if title != "Reimport Test Game (Corrected)" {
				t.Errorf("title = %q, want the corrected title", title)
			}
			if !releaseDate.Equal(correctedDate) {
				t.Errorf("release_date = %v, want %v", releaseDate, correctedDate)
			}

			var platformSlug string
			if err := sp.QueryRow(ctx,
				`SELECT p.slug FROM catalog.game_platforms gp JOIN catalog.platforms p ON p.id = gp.platform_id WHERE gp.game_id = $1`,
				firstID,
			).Scan(&platformSlug); err != nil {
				t.Fatalf("reading platform after reimport: %v", err)
			}
			if platformSlug != "reimport-test-ps4" {
				t.Errorf("platform slug = %q, want reimport-test-ps4 (old platform should have been replaced)", platformSlug)
			}
			if n := countRows(t, sp, `SELECT count(*) FROM catalog.game_platforms WHERE game_id = $1`, firstID); n != 1 {
				t.Errorf("game_platforms rows = %d, want 1 (stale association should be gone)", n)
			}
		})
	})

	t.Run("parent_game_id resolves when the parent was already imported", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			parent := NormalizedGame{IGDBID: 9003, Title: "Parent Game", Slug: "parent-game-9003", GameType: "main_game"}
			parentID, err := ImportGame(ctx, sp, parent)
			if err != nil {
				t.Fatalf("importing parent: %v", err)
			}

			parentIGDBID := int64(9003)
			child := NormalizedGame{
				IGDBID:           9004,
				Title:            "Child DLC",
				Slug:             "child-dlc-9004",
				GameType:         "dlc",
				ParentGameIGDBID: &parentIGDBID,
			}
			childID, err := ImportGame(ctx, sp, child)
			if err != nil {
				t.Fatalf("importing child: %v", err)
			}

			var gotParentID *int64
			if err := sp.QueryRow(ctx, `SELECT parent_game_id FROM catalog.games WHERE id = $1`, childID).Scan(&gotParentID); err != nil {
				t.Fatalf("reading child's parent_game_id: %v", err)
			}
			if gotParentID == nil || *gotParentID != parentID {
				t.Errorf("parent_game_id = %v, want %d", gotParentID, parentID)
			}
		})
	})

	t.Run("parent_game_id stays null when the parent isn't imported yet, and self-heals on reimport", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			missingParentIGDBID := int64(9006)
			child := NormalizedGame{
				IGDBID:           9005,
				Title:            "Orphan DLC",
				Slug:             "orphan-dlc-9005",
				GameType:         "dlc",
				ParentGameIGDBID: &missingParentIGDBID,
			}
			childID, err := ImportGame(ctx, sp, child)
			if err != nil {
				t.Fatalf("importing child before parent exists: %v", err)
			}

			var gotParentID *int64
			if err := sp.QueryRow(ctx, `SELECT parent_game_id FROM catalog.games WHERE id = $1`, childID).Scan(&gotParentID); err != nil {
				t.Fatalf("reading child's parent_game_id: %v", err)
			}
			if gotParentID != nil {
				t.Fatalf("parent_game_id = %v, want nil (parent not imported yet)", gotParentID)
			}

			parent := NormalizedGame{IGDBID: 9006, Title: "Late Parent", Slug: "late-parent-9006", GameType: "main_game"}
			parentID, err := ImportGame(ctx, sp, parent)
			if err != nil {
				t.Fatalf("importing parent: %v", err)
			}

			if _, err := ImportGame(ctx, sp, child); err != nil {
				t.Fatalf("reimporting child: %v", err)
			}
			if err := sp.QueryRow(ctx, `SELECT parent_game_id FROM catalog.games WHERE id = $1`, childID).Scan(&gotParentID); err != nil {
				t.Fatalf("reading child's parent_game_id after reimport: %v", err)
			}
			if gotParentID == nil || *gotParentID != parentID {
				t.Errorf("parent_game_id after reimport = %v, want %d", gotParentID, parentID)
			}
		})
	})

	t.Run("a platform shared by two games reuses the same row instead of duplicating it", func(t *testing.T) {
		withSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			sharedPlatform := GameRef{IGDBID: 6, Name: "PC (Microsoft Windows)", Slug: "shared-test-pc"}

			a, err := ImportGame(ctx, sp, NormalizedGame{IGDBID: 9007, Title: "Game A", Slug: "game-a-9007", GameType: "main_game", Platforms: []GameRef{sharedPlatform}})
			if err != nil {
				t.Fatalf("importing game A: %v", err)
			}
			b, err := ImportGame(ctx, sp, NormalizedGame{IGDBID: 9008, Title: "Game B", Slug: "game-b-9008", GameType: "main_game", Platforms: []GameRef{sharedPlatform}})
			if err != nil {
				t.Fatalf("importing game B: %v", err)
			}

			if n := countRows(t, sp, `SELECT count(*) FROM catalog.platforms WHERE slug = $1`, sharedPlatform.Slug); n != 1 {
				t.Errorf("catalog.platforms rows for shared slug = %d, want 1", n)
			}

			var platformIDForA, platformIDForB int
			if err := sp.QueryRow(ctx, `SELECT platform_id FROM catalog.game_platforms WHERE game_id = $1`, a).Scan(&platformIDForA); err != nil {
				t.Fatalf("reading game A's platform: %v", err)
			}
			if err := sp.QueryRow(ctx, `SELECT platform_id FROM catalog.game_platforms WHERE game_id = $1`, b).Scan(&platformIDForB); err != nil {
				t.Fatalf("reading game B's platform: %v", err)
			}
			if platformIDForA != platformIDForB {
				t.Errorf("platform ids differ (%d vs %d), want the same shared row", platformIDForA, platformIDForB)
			}
		})
	})
}
