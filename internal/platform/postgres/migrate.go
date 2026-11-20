package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/TomasSorgetti/auto-factura-api/internal/db/migrations"
)

// Migrator applies the embedded migrations. Close releases its own
// database/sql handle but not the pool, which belongs to the caller.
type Migrator struct {
	provider *goose.Provider
}

// NewMigrator builds a migrator over pool. With withLock, goose holds a
// session advisory lock while it migrates.
func NewMigrator(pool *pgxpool.Pool, withLock bool) (*Migrator, error) {
	var opts []goose.ProviderOption
	if withLock {
		locker, err := lock.NewPostgresSessionLocker()
		if err != nil {
			return nil, fmt.Errorf("new session locker: %w", err)
		}
		opts = append(opts, goose.WithSessionLocker(locker))
	}
	db := stdlib.OpenDBFromPool(pool)
	p, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS, opts...)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("new migration provider: %w", err), closeDB(db))
	}
	return &Migrator{provider: p}, nil
}

func closeDB(db *sql.DB) error {
	if err := db.Close(); err != nil {
		return fmt.Errorf("close database handle: %w", err)
	}
	return nil
}

func (m *Migrator) Up(ctx context.Context) ([]*goose.MigrationResult, error) {
	return m.provider.Up(ctx)
}

// Down reverts a single version.
func (m *Migrator) Down(ctx context.Context) (*goose.MigrationResult, error) {
	return m.provider.Down(ctx)
}

func (m *Migrator) Status(ctx context.Context) ([]*goose.MigrationStatus, error) {
	return m.provider.Status(ctx)
}

func (m *Migrator) Close() error { return m.provider.Close() }

// CheckSchema fails when the embedded migrations include versions that the
// database has not applied. A schema newer than the binary is not an error.
//
// It only reads: a database without the goose version table is reported as
// having no schema instead of being initialised.
func CheckSchema(ctx context.Context, pool *pgxpool.Pool) (err error) {
	var hasVersionTable bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.goose_db_version') IS NOT NULL`).Scan(&hasVersionTable); err != nil {
		return fmt.Errorf("check schema: %w", err)
	}
	if !hasVersionTable {
		return errors.New("database has no schema: run go run ./cmd/migrate up")
	}

	m, err := NewMigrator(pool, false)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, m.Close()) }()

	pending, err := m.provider.HasPending(ctx)
	if err != nil {
		return fmt.Errorf("check pending migrations: %w", err)
	}
	if pending {
		return errors.New("database has pending migrations: run go run ./cmd/migrate up")
	}
	return nil
}
