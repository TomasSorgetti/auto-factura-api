package main

import (
	"context"
	"errors"
	"io"
	"net/http"

	"golang.org/x/sync/errgroup"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/config"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
)

func run(ctx context.Context, getenv func(string) string, stdout io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	log := logx.New(stdout, cfg.LogLevel, cfg.LogFormat)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":{"status":"ok"}}`)
	})

	srv := newServer(cfg, mux, log)

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	g.Go(func() error {
		<-gctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return srv.Shutdown(sctx)
	})

	log.Info("api listening", "addr", cfg.HTTPAddr, "env", cfg.Env)
	err = g.Wait()
	log.Info("api stopped")
	return err
}
