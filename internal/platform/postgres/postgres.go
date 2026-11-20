// Package postgres opens the process connection pool and runs transactions.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
)

type Config struct {
	URL              logx.Secret
	MaxConns         int32
	StatementTimeout time.Duration
	AppName          string
}

// New opens a pool with bounded connections and server-side timeouts and
// verifies it with a ping. Errors never include the connection string.
func New(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	pc, err := pgxpool.ParseConfig(cfg.URL.Reveal())
	if err != nil {
		return nil, errors.New("parse database url")
	}
	pc.MaxConns = cfg.MaxConns
	pc.MinConns = 0
	pc.MaxConnLifetime = 30 * time.Minute
	pc.MaxConnIdleTime = 5 * time.Minute
	pc.HealthCheckPeriod = time.Minute
	pc.ConnConfig.ConnectTimeout = 5 * time.Second
	pc.ConnConfig.RuntimeParams["application_name"] = cfg.AppName
	pc.ConnConfig.RuntimeParams["statement_timeout"] = strconv.FormatInt(cfg.StatementTimeout.Milliseconds(), 10)
	pc.ConnConfig.RuntimeParams["lock_timeout"] = "2000"
	pc.ConnConfig.RuntimeParams["idle_in_transaction_session_timeout"] = "10000"
	pc.ConnConfig.RuntimeParams["timezone"] = "UTC"

	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, errors.New("create postgres pool")
	}

	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

// WithTx runs fn in a transaction: it commits when fn returns nil and rolls
// back on error, panic (re-raised) or runtime.Goexit. fn must use tx for all
// queries and never ask the pool for another connection.
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) (err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		p := recover()
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if rbErr := tx.Rollback(rctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) && p == nil && err != nil {
			err = errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
		}
		if p != nil {
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	return nil
}
