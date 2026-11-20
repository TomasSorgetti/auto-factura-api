// Package queue configures the River clients and its schema.
package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"
)

const (
	QueueARCA    = "arca"
	QueuePDF     = "pdf"
	QueueEmail   = "email"
	QueueDefault = river.QueueDefault
)

type Client = river.Client[pgx.Tx]

// Queues returns the queue sizes of the worker: arca 1, pdf 1, email 2, default 2.
func Queues() map[string]river.QueueConfig {
	return map[string]river.QueueConfig{
		QueueARCA:    {MaxWorkers: 1},
		QueuePDF:     {MaxWorkers: 1},
		QueueEmail:   {MaxWorkers: 2},
		QueueDefault: {MaxWorkers: 2},
	}
}

// NewInsertOnly returns a client that only inserts jobs; it is never started.
func NewInsertOnly(pool *pgxpool.Pool, log *slog.Logger) (*Client, error) {
	c, err := river.NewClient(riverpgxv5.New(pool), &river.Config{Logger: log})
	if err != nil {
		return nil, fmt.Errorf("new insert-only river client: %w", err)
	}
	return c, nil
}

// NewProcessor returns the worker client: Queues(), JobTimeout 2m, completed jobs kept 30 days,
// cancelled and discarded 180 days, and the logging middleware.
func NewProcessor(pool *pgxpool.Pool, workers *river.Workers, periodic []*river.PeriodicJob, log *slog.Logger) (*Client, error) {
	c, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger:                      log,
		Queues:                      Queues(),
		Workers:                     workers,
		PeriodicJobs:                periodic,
		JobTimeout:                  2 * time.Minute,
		CompletedJobRetentionPeriod: 30 * 24 * time.Hour,
		CancelledJobRetentionPeriod: 180 * 24 * time.Hour,
		DiscardedJobRetentionPeriod: 180 * 24 * time.Hour,
		Plugins:                     []rivertype.Plugin{&jobLogging{}},
	})
	if err != nil {
		return nil, fmt.Errorf("new river processor: %w", err)
	}
	return c, nil
}

func newMigrator(pool *pgxpool.Pool) (*rivermigrate.Migrator[pgx.Tx], error) {
	m, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return nil, fmt.Errorf("new river migrator: %w", err)
	}
	return m, nil
}

// Migrate applies River's pending migrations (line main).
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	m, err := newMigrator(pool)
	if err != nil {
		return err
	}
	if _, err := m.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("migrate river up: %w", err)
	}
	return nil
}

// CheckSchema fails when River's schema is missing or behind the library, without changing it.
func CheckSchema(ctx context.Context, pool *pgxpool.Pool) error {
	m, err := newMigrator(pool)
	if err != nil {
		return err
	}
	res, err := m.Validate(ctx, nil)
	if err != nil {
		return fmt.Errorf("check river schema: %w", err)
	}
	if !res.OK {
		return errors.New("river schema is missing or behind: run go run ./cmd/migrate up")
	}
	return nil
}

// Version returns the highest River migration version applied, or 0 when there is none.
func Version(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	m, err := newMigrator(pool)
	if err != nil {
		return 0, err
	}
	existing, err := m.ExistingVersions(ctx)
	if err != nil {
		return 0, fmt.Errorf("river versions: %w", err)
	}
	v := 0
	for _, e := range existing {
		v = max(v, e.Version)
	}
	return v, nil
}
