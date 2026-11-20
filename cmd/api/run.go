package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/TomasSorgetti/auto-factura-api/internal/db/sqlc"
	"github.com/TomasSorgetti/auto-factura-api/internal/health"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/config"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/httpx"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres"
)

func run(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	log := logx.New(stdout, cfg.LogLevel, cfg.LogFormat)

	pool, err := postgres.New(ctx, postgres.Config{
		URL:              cfg.DatabaseURL,
		MaxConns:         cfg.DBMaxConns,
		StatementTimeout: cfg.DBStatementTimeout,
		AppName:          "autofactura-api",
	})
	if err != nil {
		return err
	}
	defer pool.Close()

	sctx, scancel := context.WithTimeout(ctx, 10*time.Second)
	err = postgres.CheckSchema(sctx, pool)
	scancel()
	if err != nil {
		return err
	}

	queries := sqlc.New(pool)
	checks := []health.Check{{Name: "postgres", Ping: func(ctx context.Context) error {
		_, err := queries.Ping(ctx)
		return err
	}}}

	errs := httpx.NewErrorMapper(log, errorRules())
	rt, err := newRouter(routerDeps{log: log, errors: errs, checks: checks})
	if err != nil {
		return err
	}
	h, err := newHandler(cfg, log, errs, rt)
	if err != nil {
		return err
	}

	srv := newServer(cfg, h, log)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	g.Go(func() error {
		<-gctx.Done()
		shctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return srv.Shutdown(shctx)
	})

	log.Info("api listening", "addr", cfg.HTTPAddr, "env", cfg.Env)
	err = g.Wait()
	log.Info("api stopped")
	return err
}
