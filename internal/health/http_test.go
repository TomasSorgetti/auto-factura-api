package health

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/httpx"
)

func newTestRouter(t *testing.T, timeout time.Duration, checks ...Check) *httpx.Router {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	errs := httpx.NewErrorMapper(log, nil)
	rt, err := httpx.NewRouter(httpx.RouterConfig{Errors: errs, RequireAdmin: denyAll(errs)})
	if err != nil {
		t.Fatal(err)
	}
	NewHandler(log, timeout, checks...).Register(rt)
	return rt
}

func get(rt http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func pingOK(context.Context) error { return nil }

type readyBody struct {
	Data struct {
		Status string            `json:"status"`
		Checks map[string]string `json:"checks"`
	} `json:"data"`
	Error struct {
		Code   string            `json:"code"`
		Fields map[string]string `json:"fields"`
	} `json:"error"`
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) readyBody {
	t.Helper()
	var b readyBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	return b
}

func TestHealthz(t *testing.T) {
	t.Parallel()

	failing := Check{Name: "postgres", Ping: func(context.Context) error { return errors.New("boom") }}
	rec := get(newTestRouter(t, time.Second, failing), "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"data":{"status":"ok"}}` {
		t.Fatalf("body = %s", got)
	}
}

func TestReady(t *testing.T) {
	t.Parallel()

	secret := errors.New("password=hunter2")
	tests := []struct {
		name       string
		checks     []Check
		wantStatus int
		wantChecks map[string]string
	}{
		{"no checks", nil, 200, map[string]string{}},
		{"all ok", []Check{{"postgres", pingOK}, {"redis", pingOK}}, 200, map[string]string{"postgres": "ok", "redis": "ok"}},
		{"one fails", []Check{{"postgres", func(context.Context) error { return secret }}, {"redis", pingOK}}, 503, map[string]string{"postgres": "down", "redis": "ok"}},
	}
	for _, tt := range tests {
		for _, path := range []string{"/readyz", "/api/health"} {
			t.Run(tt.name+path, func(t *testing.T) {
				t.Parallel()
				rec := get(newTestRouter(t, time.Second, tt.checks...), path)
				if rec.Code != tt.wantStatus {
					t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
				}
				if strings.Contains(rec.Body.String(), "hunter2") {
					t.Fatalf("body leaks error: %s", rec.Body)
				}
				b := decode(t, rec)
				if tt.wantStatus == http.StatusOK {
					if b.Data.Status != "ok" || !maps.Equal(b.Data.Checks, tt.wantChecks) {
						t.Fatalf("data = %+v", b.Data)
					}
					if !strings.Contains(rec.Body.String(), `"checks"`) {
						t.Fatalf("checks missing: %s", rec.Body)
					}
					return
				}
				if b.Error.Code != "SERVICE_NOT_READY" || !maps.Equal(b.Error.Fields, tt.wantChecks) {
					t.Fatalf("error = %+v", b.Error)
				}
			})
		}
	}
}

func TestReadyTimeout(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		wait := func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}
		rt := newTestRouter(t, 2*time.Second, Check{"postgres", wait}, Check{"redis", pingOK})
		rec := get(rt, "/readyz")
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d", rec.Code)
		}
		b := decode(t, rec)
		if !maps.Equal(b.Error.Fields, map[string]string{"postgres": "down", "redis": "ok"}) {
			t.Fatalf("fields = %v", b.Error.Fields)
		}
	})
}

func denyAll(m *httpx.ErrorMapper) httpx.Middleware {
	return func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			m.Write(w, r, httpx.ErrUnauthenticated)
		})
	}
}
