package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/emails"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/config"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/queue"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/secrets"
)

func run(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	cfg, err := config.LoadWorker(getenv)
	if err != nil {
		return err
	}
	log := logx.New(stdout, cfg.LogLevel, cfg.LogFormat)

	pool, err := postgres.New(ctx, postgres.Config{
		URL:              cfg.DatabaseURL,
		MaxConns:         cfg.DBMaxConns,
		StatementTimeout: cfg.DBStatementTimeout,
		AppName:          "autofactura-worker",
	})
	if err != nil {
		return err
	}
	hardStopped := false
	defer func() {
		if !hardStopped {
			pool.Close()
			return
		}
		closed := make(chan struct{})
		go func() {
			pool.Close()
			close(closed)
		}()
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-closed:
		case <-timer.C:
			log.Warn("postgres pool close timed out; a job still holds a connection")
		}
	}()

	sctx, scancel := context.WithTimeout(ctx, 10*time.Second)
	err = postgres.CheckSchema(sctx, pool)
	if err == nil {
		err = queue.CheckSchema(sctx, pool)
	}
	scancel()
	if err != nil {
		return err
	}

	hasher, err := secrets.NewPasswordHasher(cfg.Argon2)
	if err != nil {
		return err
	}
	workers, periodic, err := newWorkers(workerDeps{
		pool: pool, hasher: hasher, log: log,
		sender: emails.NewSender(cfg.ResendAPIKey, cfg.EmailFrom, log),
	})
	if err != nil {
		return err
	}
	client, err := queue.NewProcessor(pool, workers, periodic, log)
	if err != nil {
		return err
	}

	if err := client.Start(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("start river: %w", err)
	}
	log.Info("worker started", "env", cfg.Env)

	select {
	case <-client.Stopped():
		return errors.New("river stopped unexpectedly")
	case <-ctx.Done():
	}

	log.Info("worker stopping")
	stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
	err = client.Stop(stopCtx)
	cancel()
	if err != nil {
		log.Warn("graceful stop timed out; cancelling running jobs", "err", err)
		hardCtx, hardCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = client.StopAndCancel(hardCtx)
		hardCancel()
		if err != nil {
			hardStopped = true
			log.Error("river hard stop timed out", "err", err)
			return fmt.Errorf("stop river: %w", err)
		}
	}
	log.Info("worker stopped")
	return nil
}
