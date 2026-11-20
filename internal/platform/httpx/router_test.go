package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func newRouter(t *testing.T) *Router {
	t.Helper()
	log, _ := newTestLogger()
	m := NewErrorMapper(log, nil)
	rt, err := NewRouter(RouterConfig{Errors: m, RequireAdmin: denyAll(m)})
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

func mustPanic(t *testing.T, name string, f func()) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		t.Parallel()
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic")
			}
		}()
		f()
	})
}

func TestRouter(t *testing.T) {
	t.Parallel()

	ok := func(w http.ResponseWriter, r *http.Request) error { return JSON(w, 200, "ok") }

	mustPanic(t, "admin wrong prefix", func() { newRouter(t).Admin("GET /api/other", ok) })
	mustPanic(t, "system under admin", func() { newRouter(t).System("GET /api/admin/x", ok) })
	mustPanic(t, "system under public", func() { newRouter(t).System("GET /api/public/x", ok) })
	mustPanic(t, "pattern without method", func() { newRouter(t).System("/healthz", ok) })

	t.Run("new router without admin middleware", func(t *testing.T) {
		t.Parallel()
		log, _ := newTestLogger()
		if _, err := NewRouter(RouterConfig{Errors: NewErrorMapper(log, nil)}); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("serving", func(t *testing.T) {
		t.Parallel()
		rt := newRouter(t)
		rt.Admin("GET /api/admin/things", ok)
		rt.System("GET /healthz", ok)
		rt.System("GET /boom", func(http.ResponseWriter, *http.Request) error { return ErrForbidden })
		rt.System("GET /late", func(w http.ResponseWriter, r *http.Request) error {
			w.WriteHeader(http.StatusOK)
			return errors.New("late")
		})

		tests := []struct {
			name, method, path, code, allow string
			status                          int
		}{
			{name: "admin denied", method: "GET", path: "/api/admin/things", status: 401, code: "UNAUTHENTICATED"},
			{name: "handler error", method: "GET", path: "/boom", status: 403, code: "FORBIDDEN"},
			{name: "not found", method: "GET", path: "/nope", status: 404, code: "NOT_FOUND"},
			{name: "method not allowed", method: "POST", path: "/healthz", status: 405, code: "METHOD_NOT_ALLOWED", allow: "GET, HEAD"},
			{name: "ok", method: "GET", path: "/healthz", status: 200},
			{name: "error after response started", method: "GET", path: "/late", status: 200},
		}
		for _, tt := range tests {
			rec := httptest.NewRecorder()
			rt.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.status {
				t.Fatalf("%s: status %d, want %d", tt.name, rec.Code, tt.status)
			}
			if tt.code != "" && readEnvelope(t, rec).Error.Code != tt.code {
				t.Fatalf("%s: body %q", tt.name, rec.Body.String())
			}
			if tt.allow != "" && rec.Header().Get("Allow") != tt.allow {
				t.Fatalf("%s: allow %q", tt.name, rec.Header().Get("Allow"))
			}
		}
	})

	t.Run("system wildcard cannot serve admin paths", func(t *testing.T) {
		t.Parallel()
		rt := newRouter(t)
		called := false
		h := func(w http.ResponseWriter, r *http.Request) error {
			called = true
			return JSON(w, 200, "ok")
		}
		rt.System("GET /api/{surface}/clients/{id}", h)
		rt.System("POST /{rest...}", h)
		for _, c := range []struct{ method, path string }{
			{"GET", "/api/admin/clients/1"},
			{"POST", "/api/admin/clients"},
		} {
			rec := httptest.NewRecorder()
			rt.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
			if rec.Code != 404 || readEnvelope(t, rec).Error.Code != "NOT_FOUND" {
				t.Fatalf("%s %s: got %d %q", c.method, c.path, rec.Code, rec.Body.String())
			}
		}
		if called {
			t.Fatal("system handler ran for an admin path")
		}
		rec := httptest.NewRecorder()
		rt.ServeHTTP(rec, httptest.NewRequest("GET", "/api/other/clients/1", nil))
		if rec.Code != 200 {
			t.Fatalf("non-admin path: got %d", rec.Code)
		}
	})

	t.Run("routes", func(t *testing.T) {
		t.Parallel()
		rt := newRouter(t)
		rt.Admin("POST /api/admin/b", ok)
		rt.System("GET /healthz", ok)
		rt.Admin("GET /api/admin/b", ok)
		want := []Route{
			{"GET", "/api/admin/b", SurfaceAdmin},
			{"POST", "/api/admin/b", SurfaceAdmin},
			{"GET", "/healthz", SurfaceSystem},
		}
		if got := rt.Routes(); !slices.Equal(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
}
