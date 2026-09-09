package jobs

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

type TestJobArgs struct {
	Message string `json:"message"`
}

func (TestJobArgs) Kind() string { return "test" }

type TestJobWorker struct {
	river.WorkerDefaults[TestJobArgs]
	Logger *slog.Logger
}

func (w *TestJobWorker) Work(ctx context.Context, job *river.Job[TestJobArgs]) error {
	w.Logger.InfoContext(ctx, "jobs: processed test job",
		"job_id", job.ID,
		"attempt", job.Attempt,
		"message", job.Args.Message,
	)
	return nil
}

func NewWorkerClient(pool *pgxpool.Pool, logger *slog.Logger) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &TestJobWorker{Logger: logger})

	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 10},
		},
		Workers: workers,
	})
}

func NewInsertClient(pool *pgxpool.Pool) (*river.Client[pgx.Tx], error) {
	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 1},
		},
		Workers: river.NewWorkers(),
	})
}
