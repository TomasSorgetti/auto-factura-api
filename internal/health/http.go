package health

import (
	"context"
	"net/http"
	"sync"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/httpx"
)

const (
	statusOK   = "ok"
	statusDown = "down"
)

type statusResponse struct {
	Status string `json:"status"`
}

type readyResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func (h *Handler) Register(rt *httpx.Router) {
	rt.System("GET /healthz", h.live)
	rt.System("GET /readyz", h.ready)
	rt.System("GET /api/health", h.ready)
}

func (h *Handler) live(w http.ResponseWriter, _ *http.Request) error {
	return httpx.JSON(w, http.StatusOK, statusResponse{Status: statusOK})
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	results := make([]error, len(h.checks))
	var wg sync.WaitGroup
	for i, c := range h.checks {
		wg.Go(func() {
			results[i] = c.Ping(ctx)
		})
	}
	wg.Wait()

	checks := make(map[string]string, len(h.checks))
	failed := false
	for i, c := range h.checks {
		if results[i] != nil {
			failed = true
			checks[c.Name] = statusDown
			h.log.WarnContext(ctx, "readiness check failed", "check", c.Name, "err", results[i])
			continue
		}
		checks[c.Name] = statusOK
	}
	if failed {
		return httpx.ErrServiceNotReady.WithFields(checks)
	}
	return httpx.JSON(w, http.StatusOK, readyResponse{Status: statusOK, Checks: checks})
}
