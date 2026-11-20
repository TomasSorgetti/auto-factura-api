package main

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/auth"
	"github.com/TomasSorgetti/auto-factura-api/internal/health"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/httpx"
	"github.com/TomasSorgetti/auto-factura-api/internal/settings"
)

type routerDeps struct {
	log          *slog.Logger
	errors       *httpx.ErrorMapper
	checks       []health.Check
	auth         *auth.Handler
	settings     *settings.Handler
	requireAdmin httpx.Middleware
}

func newRouter(d routerDeps) (*httpx.Router, error) {
	if d.auth == nil {
		return nil, errors.New("new router: auth handler is required")
	}
	if d.settings == nil {
		return nil, errors.New("new router: settings handler is required")
	}
	rt, err := httpx.NewRouter(httpx.RouterConfig{
		Errors:       d.errors,
		RequireAdmin: d.requireAdmin,
	})
	if err != nil {
		return nil, fmt.Errorf("new router: %w", err)
	}
	health.NewHandler(d.log, 2*time.Second, d.checks...).Register(rt)
	d.auth.Register(rt)
	d.settings.Register(rt)
	return rt, nil
}
