package main

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/TomasSorgetti/auto-factura-api/internal/auth"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/secrets"
)

type workerDeps struct {
	pool   *pgxpool.Pool
	hasher *secrets.PasswordHasher
	log    *slog.Logger
}

func newWorkers(d workerDeps) (*river.Workers, []*river.PeriodicJob, error) {
	workers := river.NewWorkers()
	accounts := auth.NewAccounts(d.pool, d.hasher)
	if err := river.AddWorkerSafely(workers, &auth.PruneTokensWorker{Accounts: accounts, Log: d.log}); err != nil {
		return nil, nil, err
	}
	return workers, []*river.PeriodicJob{auth.PruneTokensPeriodic()}, nil
}
