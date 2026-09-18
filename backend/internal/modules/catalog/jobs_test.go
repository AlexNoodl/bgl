package catalog

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"bgl/internal/platform/db"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

func newTestJob(id int64, args ImportGameMetadataArgs) *river.Job[ImportGameMetadataArgs] {
	return &river.Job[ImportGameMetadataArgs]{
		JobRow: &rivertype.JobRow{ID: id},
		Args:   args,
	}
}

func TestImportGameMetadataWorker_Work_NoIGDBConfiguredIsANoOp(t *testing.T) {
	w := &ImportGameMetadataWorker{Pool: nil, IGDB: nil, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	err := w.Work(context.Background(), newTestJob(1, ImportGameMetadataArgs{IGDBID: 12345}))
	if err != nil {
		t.Fatalf("Work with nil IGDB client: %v", err)
	}
}

func TestImportGameMetadataWorker_Work_ImportsFullMetadata(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping (see docs/backlog.md GAME-001)")
	}

	ctx := context.Background()

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("creating pool: %v", err)
	}
	t.Cleanup(pool.Close)

	const gamePayload = `[{
		"id": 555001,
		"name": "Worker Test Game",
		"slug": "worker-test-game-555001",
		"summary": "A game used to test the full-metadata import job.",
		"first_release_date": 1000000000,
		"cover": {"id": 1, "image_id": "worker-test-cover"},
		"screenshots": [{"id": 1, "image_id": "worker-test-shot-1"}, {"id": 2, "image_id": "worker-test-shot-2"}],
		"platforms": [{"id": 6, "name": "PC (Microsoft Windows)", "slug": "worker-test-pc"}],
		"genres": [{"id": 12, "name": "Role-playing (RPG)", "slug": "worker-test-rpg"}],
		"involved_companies": [
			{"id": 1, "company": {"id": 1012, "name": "FromSoftware", "slug": "worker-test-fromsoftware"}, "developer": true, "publisher": true}
		]
	}]`

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/oauth2/token" {
			return jsonResponse(http.StatusOK, `{"access_token":"tok-1","expires_in":3600,"token_type":"bearer"}`), nil
		}
		return jsonResponse(http.StatusOK, gamePayload), nil
	})
	igdbClient := newTestClient(t, transport, nil)

	w := &ImportGameMetadataWorker{Pool: pool, IGDB: igdbClient, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	t.Cleanup(func() {
		ctx := context.Background()
		if _, err := pool.Exec(ctx, `DELETE FROM catalog.games WHERE slug = 'worker-test-game-555001'`); err != nil {
			t.Errorf("cleanup: deleting test game: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM catalog.platforms WHERE slug = 'worker-test-pc'`); err != nil {
			t.Errorf("cleanup: deleting test platform: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM catalog.genres WHERE slug = 'worker-test-rpg'`); err != nil {
			t.Errorf("cleanup: deleting test genre: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM catalog.companies WHERE slug = 'worker-test-fromsoftware'`); err != nil {
			t.Errorf("cleanup: deleting test company: %v", err)
		}
	})

	if err := w.Work(ctx, newTestJob(1, ImportGameMetadataArgs{IGDBID: 555001})); err != nil {
		t.Fatalf("Work: %v", err)
	}

	var gameID int64
	var title string
	if err := pool.QueryRow(ctx, `SELECT id, title FROM catalog.games WHERE slug = 'worker-test-game-555001'`).Scan(&gameID, &title); err != nil {
		t.Fatalf("reading imported game: %v", err)
	}
	if title != "Worker Test Game" {
		t.Errorf("title = %q", title)
	}

	assertCount := func(query string) {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, query, gameID).Scan(&n); err != nil {
			t.Fatalf("counting (%s): %v", query, err)
		}
		if n == 0 {
			t.Errorf("expected at least one row for %q", query)
		}
	}
	assertCount(`SELECT count(*) FROM catalog.game_platforms WHERE game_id = $1`)
	assertCount(`SELECT count(*) FROM catalog.game_genres WHERE game_id = $1`)
	assertCount(`SELECT count(*) FROM catalog.game_companies WHERE game_id = $1`)

	var mediaCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM catalog.game_media WHERE game_id = $1`, gameID).Scan(&mediaCount); err != nil {
		t.Fatalf("counting game_media: %v", err)
	}
	if mediaCount != 3 { // 1 cover + 2 screenshots
		t.Errorf("game_media rows = %d, want 3", mediaCount)
	}

	if err := w.Work(ctx, newTestJob(2, ImportGameMetadataArgs{IGDBID: 555001})); err != nil {
		t.Fatalf("second Work: %v", err)
	}
	var gameCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM catalog.games WHERE slug = 'worker-test-game-555001'`).Scan(&gameCount); err != nil {
		t.Fatalf("counting games: %v", err)
	}
	if gameCount != 1 {
		t.Errorf("games rows for this slug = %d, want 1 (no duplicate from reimport)", gameCount)
	}
}
