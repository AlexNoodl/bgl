package jobs

import (
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

type WorkerRegistrar func(workers *river.Workers)

func NewWorkerClient(pool *pgxpool.Pool, logger *slog.Logger, registrars ...WorkerRegistrar) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	for _, register := range registrars {
		register(workers)
	}

	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Schema: "jobs",
		Logger: logger,
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 10},
		},
		Workers: workers,
	})
}

func NewInsertClient(pool *pgxpool.Pool) (*river.Client[pgx.Tx], error) {
	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Schema: "jobs",
	})
}
