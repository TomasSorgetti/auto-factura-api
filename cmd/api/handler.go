package main

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/config"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/httpx"
)

func newHandler(cfg config.Config, log *slog.Logger, errs *httpx.ErrorMapper, rt *httpx.Router) (http.Handler, error) {
	crossOrigin, err := httpx.CrossOrigin(cfg.CORSAllowedOrigins, errs)
	if err != nil {
		return nil, fmt.Errorf("cross-origin protection: %w", err)
	}
	return httpx.Chain(rt,
		httpx.Recover(log, errs),
		httpx.RequestID(),
		httpx.Logger(log),
		httpx.SecurityHeaders(),
		httpx.CORS(cfg.CORSAllowedOrigins, errs),
		httpx.Timeout(cfg.RequestTimeout),
		crossOrigin,
	), nil
}
