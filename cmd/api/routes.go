package main

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/health"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/httpx"
)

type routerDeps struct {
	log    *slog.Logger
	errors *httpx.ErrorMapper
	checks []health.Check
}

func newRouter(d routerDeps) (*httpx.Router, error) {
	rt, err := httpx.NewRouter(httpx.RouterConfig{
		Errors:       d.errors,
		RequireAdmin: httpx.DenyAll(d.errors),
	})
	if err != nil {
		return nil, fmt.Errorf("new router: %w", err)
	}
	health.NewHandler(d.log, 2*time.Second, d.checks...).Register(rt)
	return rt, nil
}
