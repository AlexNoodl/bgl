package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"bgl/internal/platform/jobs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

const fullGameMetadataFields = `fields name,slug,summary,storyline,first_release_date,category,parent_game,` +
	`platforms.name,platforms.slug,genres.name,genres.slug,` +
	`involved_companies.company.name,involved_companies.company.slug,` +
	`involved_companies.developer,involved_companies.publisher,involved_companies.porting,involved_companies.supporting,` +
	`cover.image_id,screenshots.image_id;`

type ImportGameMetadataArgs struct {
	IGDBID int64 `json:"igdb_id"`
}

func (ImportGameMetadataArgs) Kind() string { return "catalog.import_game_metadata" }

type ImportGameMetadataWorker struct {
	river.WorkerDefaults[ImportGameMetadataArgs]
	Pool   *pgxpool.Pool
	IGDB   *IGDBClient
	Logger *slog.Logger
}

func (w *ImportGameMetadataWorker) Work(ctx context.Context, job *river.Job[ImportGameMetadataArgs]) error {
	if w.IGDB == nil {
		w.Logger.WarnContext(ctx, "catalog: would import full game metadata (no IGDB credentials configured — set IGDB_CLIENT_ID/IGDB_CLIENT_SECRET)",
			"job_id", job.ID,
			"igdb_id", job.Args.IGDBID,
		)
		return nil
	}

	query := fmt.Sprintf(`%s where id = %d;`, fullGameMetadataFields, job.Args.IGDBID)
	body, err := w.IGDB.Query(ctx, "games", query)
	if err != nil {
		return fmt.Errorf("querying igdb for game %d: %w", job.Args.IGDBID, err)
	}

	var payloads []IGDBGamePayload
	if err := json.Unmarshal(body, &payloads); err != nil {
		return fmt.Errorf("decoding igdb response for game %d: %w", job.Args.IGDBID, err)
	}
	if len(payloads) == 0 {
		return fmt.Errorf("igdb returned no game for id %d", job.Args.IGDBID)
	}
	raw := payloads[0]

	normalized := NormalizeIGDBGame(raw)

	tx, err := w.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	gameID, err := ImportGame(ctx, tx, normalized)
	if err != nil {
		return fmt.Errorf("importing game %d: %w", job.Args.IGDBID, err)
	}

	var coverImageID string
	if raw.Cover != nil {
		coverImageID = raw.Cover.ImageID
	}
	screenshotImageIDs := make([]string, len(raw.Screenshots))
	for i, s := range raw.Screenshots {
		screenshotImageIDs[i] = s.ImageID
	}
	if err := importGameMedia(ctx, tx, gameID, coverImageID, screenshotImageIDs); err != nil {
		return fmt.Errorf("importing media for game %d: %w", job.Args.IGDBID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	w.Logger.InfoContext(ctx, "catalog: imported full game metadata",
		"job_id", job.ID,
		"igdb_id", job.Args.IGDBID,
		"game_id", gameID,
		"platforms", len(normalized.Platforms),
		"genres", len(normalized.Genres),
		"companies", len(normalized.Companies),
		"screenshots", len(screenshotImageIDs),
	)
	return nil
}

func RegisterWorkers(logger *slog.Logger, pool *pgxpool.Pool, igdb *IGDBClient) jobs.WorkerRegistrar {
	return func(workers *river.Workers) {
		river.AddWorker(workers, &ImportGameMetadataWorker{Pool: pool, IGDB: igdb, Logger: logger})
	}
}
