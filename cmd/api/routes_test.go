package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/auth"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/config"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/httpx"
	"github.com/TomasSorgetti/auto-factura-api/internal/settings"
)

const zeroUUID = "00000000-0000-0000-0000-000000000000"

type denyAuthenticator struct{}

func (denyAuthenticator) Authenticate(context.Context, string) (auth.Principal, error) {
	return auth.Principal{}, auth.ErrSessionInvalid
}

func testRouterDeps(log *slog.Logger, errs *httpx.ErrorMapper) routerDeps {
	return routerDeps{
		log: log, errors: errs,
		auth:         &auth.Handler{},
		settings:     settings.NewHandler(nil),
		requireAdmin: auth.RequireAdmin(denyAuthenticator{}, errs),
	}
}

func testDeps() (*slog.Logger, *httpx.ErrorMapper) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return log, httpx.NewErrorMapper(log, errorRules())
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var b struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	return b.Error.Code
}

func fillPathValues(path string) string {
	for {
		start := strings.Index(path, "{")
		if start < 0 {
			return path
		}
		end := strings.Index(path[start:], "}")
		path = path[:start] + zeroUUID + path[start+end+1:]
	}
}

func TestAdminRoutesRequireSession(t *testing.T) {
	t.Parallel()

	log, errs := testDeps()
	rt, err := newRouter(testRouterDeps(log, errs))
	if err != nil {
		t.Fatal(err)
	}
	routes := rt.Routes()

	for _, want := range []httpx.Route{
		{Method: "GET", Path: "/healthz", Surface: httpx.SurfaceSystem},
		{Method: "GET", Path: "/readyz", Surface: httpx.SurfaceSystem},
		{Method: "GET", Path: "/api/health", Surface: httpx.SurfaceSystem},
		{Method: "POST", Path: "/api/auth/login", Surface: httpx.SurfaceSystem},
		{Method: "POST", Path: "/api/auth/totp", Surface: httpx.SurfaceSystem},
		{Method: "POST", Path: "/api/auth/totp/setup", Surface: httpx.SurfaceSystem},
		{Method: "POST", Path: "/api/auth/totp/enroll", Surface: httpx.SurfaceSystem},
		{Method: "POST", Path: "/api/auth/recovery", Surface: httpx.SurfaceSystem},
		{Method: "POST", Path: "/api/auth/refresh", Surface: httpx.SurfaceSystem},
		{Method: "POST", Path: "/api/auth/logout", Surface: httpx.SurfaceSystem},
		{Method: "GET", Path: "/api/admin/me", Surface: httpx.SurfaceAdmin},
		{Method: "GET", Path: "/api/admin/settings", Surface: httpx.SurfaceAdmin},
		{Method: "PUT", Path: "/api/admin/settings", Surface: httpx.SurfaceAdmin},
	} {
		if !slices.Contains(routes, want) {
			t.Errorf("route %+v is not registered", want)
		}
	}

	for _, route := range routes {
		isAdminPath := strings.HasPrefix(route.Path, "/api/admin/")
		if route.Surface != httpx.SurfaceAdmin {
			if isAdminPath {
				t.Errorf("%s %s is under /api/admin/ but surface is %s", route.Method, route.Path, route.Surface)
			}
			continue
		}
		if !isAdminPath {
			t.Errorf("admin route %s %s is outside /api/admin/", route.Method, route.Path)
			continue
		}
		rec := httptest.NewRecorder()
		rt.ServeHTTP(rec, httptest.NewRequest(route.Method, fillPathValues(route.Path), nil))
		if rec.Code != http.StatusUnauthorized || errorCode(t, rec) != "UNAUTHENTICATED" {
			t.Errorf("%s %s without session: status %d body %s", route.Method, route.Path, rec.Code, rec.Body)
		}
	}
}

func TestHandlerChain(t *testing.T) {
	t.Parallel()

	log, errs := testDeps()
	rt, err := newRouter(testRouterDeps(log, errs))
	if err != nil {
		t.Fatal(err)
	}
	h, err := newHandler(config.Config{RequestTimeout: time.Second}, log, errs, rt)
	if err != nil {
		t.Fatal(err)
	}
	do := func(method, path string, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := do("GET", "/healthz", nil)
	if rec.Code != 200 || rec.Header().Get("X-Request-ID") == "" || rec.Header().Get("X-Content-Type-Options") == "" {
		t.Fatalf("healthz: status %d headers %v", rec.Code, rec.Header())
	}
	if rec := do("POST", "/healthz", nil); rec.Code != 405 || errorCode(t, rec) != "METHOD_NOT_ALLOWED" {
		t.Fatalf("POST /healthz: %d %s", rec.Code, rec.Body)
	}
	if rec := do("GET", "/nope", nil); rec.Code != 404 || errorCode(t, rec) != "NOT_FOUND" {
		t.Fatalf("GET /nope: %d %s", rec.Code, rec.Body)
	}
	if rec := do("POST", "/api/admin/x", map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != 403 || errorCode(t, rec) != "CROSS_ORIGIN_REQUEST" {
		t.Fatalf("cross-site: %d %s", rec.Code, rec.Body)
	}
}
