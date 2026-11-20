package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCORS(t *testing.T) {
	t.Parallel()

	log, _ := newTestLogger()
	m := NewErrorMapper(log, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	const allowed = "https://app.example.com"

	preflight := func(origin string) *http.Request {
		r := httptest.NewRequest(http.MethodOptions, "/api/x", nil)
		r.Header.Set("Origin", origin)
		r.Header.Set("Access-Control-Request-Method", "POST")
		return r
	}

	t.Run("no origins configured", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		CORS(nil, m)(next).ServeHTTP(rec, preflight(allowed))
		if rec.Code != 200 || len(rec.Header()) != 0 {
			t.Fatalf("expected untouched, got %d %v", rec.Code, rec.Header())
		}
	})
	t.Run("allowed preflight", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		CORS([]string{allowed}, m)(next).ServeHTTP(rec, preflight(allowed))
		h := rec.Header()
		if rec.Code != 204 || h.Get("Access-Control-Allow-Origin") != allowed ||
			h.Get("Access-Control-Allow-Credentials") != "true" ||
			h.Get("Access-Control-Allow-Methods") == "" || h.Get("Access-Control-Max-Age") != "600" {
			t.Fatalf("got %d %v", rec.Code, h)
		}
		if v := strings.Join(h.Values("Vary"), ","); !strings.Contains(v, "Origin") || !strings.Contains(v, "Access-Control-Request-Method") {
			t.Fatalf("vary %q", v)
		}
	})
	t.Run("disallowed preflight", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		CORS([]string{allowed}, m)(next).ServeHTTP(rec, preflight("https://evil.example.com"))
		if rec.Code != 403 || readEnvelope(t, rec).Error.Code != "CROSS_ORIGIN_REQUEST" {
			t.Fatalf("got %d %q", rec.Code, rec.Body.String())
		}
	})
	t.Run("disallowed normal request", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Origin", "https://evil.example.com")
		CORS([]string{allowed}, m)(next).ServeHTTP(rec, r)
		if rec.Code != 200 || rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("got %d %v", rec.Code, rec.Header())
		}
		if rec.Header().Get("Vary") != "Origin" {
			t.Fatalf("vary %q", rec.Header().Get("Vary"))
		}
	})
}
