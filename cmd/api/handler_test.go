package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/auth"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/config"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/httpx"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
)

func TestHandlerPanicLogsRequest(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := logx.New(&buf, slog.LevelDebug, "json")
	errs := httpx.NewErrorMapper(log, errorRules())
	rt, err := httpx.NewRouter(httpx.RouterConfig{Errors: errs, RequireAdmin: auth.RequireAdmin(denyAuthenticator{}, errs)})
	if err != nil {
		t.Fatal(err)
	}
	rt.System("GET /boom", func(http.ResponseWriter, *http.Request) error { panic("boom") })
	h, err := newHandler(config.Config{RequestTimeout: time.Second}, log, errs, rt)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/boom", nil))

	if rec.Code != http.StatusInternalServerError || errorCode(t, rec) != "INTERNAL" {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	id := rec.Header().Get("X-Request-ID")
	if id == "" {
		t.Fatal("missing X-Request-ID")
	}

	var sawPanic, sawRequest bool
	dec := json.NewDecoder(&buf)
	for dec.More() {
		var line struct {
			Msg       string `json:"msg"`
			Status    int    `json:"status"`
			RequestID string `json:"request_id"`
		}
		if err := dec.Decode(&line); err != nil {
			t.Fatal(err)
		}
		switch line.Msg {
		case "panic":
			sawPanic = true
		case "http request":
			sawRequest = line.Status == http.StatusInternalServerError && line.RequestID == id
		}
	}
	if !sawPanic || !sawRequest {
		t.Fatalf("panic logged %v, http request 500 with request_id logged %v; log: %s", sawPanic, sawRequest, buf.String())
	}
}
