// Package health exposes the liveness and readiness endpoints.
package health

import (
	"context"
	"log/slog"
	"time"
)

// Check pings one dependency. Ping must honor ctx: readiness bounds it with a
// deadline.
type Check struct {
	Name string
	Ping func(ctx context.Context) error
}

type Handler struct {
	log     *slog.Logger
	timeout time.Duration
	checks  []Check
}

func NewHandler(log *slog.Logger, timeout time.Duration, checks ...Check) *Handler {
	return &Handler{log: log, timeout: timeout, checks: checks}
}
