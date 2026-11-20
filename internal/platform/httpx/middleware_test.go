package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestRecover(t *testing.T) {
	t.Parallel()

	t.Run("panic before write", func(t *testing.T) {
		t.Parallel()
		log, buf := newTestLogger()
		h := Recover(log, NewErrorMapper(log, nil))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if rec.Code != 500 || readEnvelope(t, rec).Error.Code != "INTERNAL" {
			t.Fatalf("got %d %q", rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "boom") || !strings.Contains(buf.String(), `"stack"`) {
			t.Fatalf("body %q log %q", rec.Body.String(), buf.String())
		}
	})
	t.Run("panic after write", func(t *testing.T) {
		t.Parallel()
		log, _ := newTestLogger()
		h := Recover(log, NewErrorMapper(log, nil))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
			panic("boom")
		}))
		rec := httptest.NewRecorder()
		func() {
			defer func() {
				if v := recover(); v != http.ErrAbortHandler {
					t.Fatalf("recovered %v, want ErrAbortHandler", v)
				}
			}()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		}()
		if rec.Code != 202 || rec.Body.Len() != 0 {
			t.Fatalf("got %d %q", rec.Code, rec.Body.String())
		}
	})
	t.Run("abort handler is re-panicked", func(t *testing.T) {
		t.Parallel()
		log, _ := newTestLogger()
		h := Recover(log, NewErrorMapper(log, nil))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }))
		defer func() {
			if v := recover(); v != http.ErrAbortHandler {
				t.Fatalf("recovered %v", v)
			}
		}()
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	})
}

func TestRequestID(t *testing.T) {
	t.Parallel()

	hex32 := regexp.MustCompile(`^[0-9a-f]{32}$`)
	tests := []struct {
		name     string
		header   string
		wantSame bool
	}{
		{name: "valid", header: "abc-123", wantSame: true},
		{name: "empty"},
		{name: "invalid chars", header: "a b$"},
		{name: "too long", header: strings.Repeat("a", 65)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := RequestID()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("X-Request-ID", tt.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			got := rec.Header().Get("X-Request-ID")
			if tt.wantSame && got != tt.header {
				t.Fatalf("got %q", got)
			}
			if !tt.wantSame && !hex32.MatchString(got) {
				t.Fatalf("got %q", got)
			}
		})
	}
}

func TestLoggerNeverLogsPath(t *testing.T) {
	t.Parallel()

	log, buf := newTestLogger()
	m := NewErrorMapper(log, nil)
	rt, err := NewRouter(RouterConfig{Errors: m, RequireAdmin: DenyAll(m)})
	if err != nil {
		t.Fatal(err)
	}
	rt.System("GET /p/{token}", func(w http.ResponseWriter, r *http.Request) error { return JSON(w, 200, "ok") })
	h := Chain(rt, Recover(log, m), RequestID(), Logger(log))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/p/secret-token?x=1", nil))
	out := buf.String()
	if strings.Contains(out, "secret-token") || strings.Contains(out, "x=1") {
		t.Fatalf("log leaks path: %s", out)
	}
	if !strings.Contains(out, "GET /p/{token}") {
		t.Fatalf("log lacks pattern: %s", out)
	}
}

func TestSecurityHeaders(t *testing.T) {
	t.Parallel()

	log, _ := newTestLogger()
	m := NewErrorMapper(log, nil)
	rt, err := NewRouter(RouterConfig{Errors: m, RequireAdmin: DenyAll(m)})
	if err != nil {
		t.Fatal(err)
	}
	h := Chain(rt, SecurityHeaders())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if rec.Code != 404 {
		t.Fatalf("status %d", rec.Code)
	}
	for _, k := range []string{"X-Content-Type-Options", "Referrer-Policy", "Cache-Control", "Content-Security-Policy", "Cross-Origin-Resource-Policy"} {
		if rec.Header().Get(k) == "" {
			t.Errorf("missing %s", k)
		}
	}
}

func TestTimeout(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		log, _ := newTestLogger()
		m := NewErrorMapper(log, nil)
		rt, err := NewRouter(RouterConfig{Errors: m, RequireAdmin: DenyAll(m)})
		if err != nil {
			t.Fatal(err)
		}
		rt.System("GET /slow", func(w http.ResponseWriter, r *http.Request) error {
			<-r.Context().Done()
			return r.Context().Err()
		})
		h := Chain(rt, Timeout(time.Second))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/slow", nil).WithContext(context.Background()))
		if rec.Code != 503 || readEnvelope(t, rec).Error.Code != "REQUEST_TIMEOUT" {
			t.Fatalf("got %d %q", rec.Code, rec.Body.String())
		}
	})
}

func TestCrossOrigin(t *testing.T) {
	t.Parallel()

	log, _ := newTestLogger()
	m := NewErrorMapper(log, nil)
	mw, err := CrossOrigin([]string{"https://trusted.example.com"}, m)
	if err != nil {
		t.Fatal(err)
	}
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	tests := []struct {
		name    string
		method  string
		headers map[string]string
		want    int
	}{
		{name: "cross-site post", method: "POST", headers: map[string]string{"Sec-Fetch-Site": "cross-site"}, want: 403},
		{name: "same-origin post", method: "POST", headers: map[string]string{"Sec-Fetch-Site": "same-origin"}, want: 200},
		{name: "cross-site get", method: "GET", headers: map[string]string{"Sec-Fetch-Site": "cross-site"}, want: 200},
		{name: "trusted origin", method: "POST", headers: map[string]string{"Origin": "https://trusted.example.com"}, want: 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tt.method, "https://api.example.com/x", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status %d, want %d", rec.Code, tt.want)
			}
			if tt.want == 403 && readEnvelope(t, rec).Error.Code != "CROSS_ORIGIN_REQUEST" {
				t.Fatalf("body %q", rec.Body.String())
			}
		})
	}
}

func TestChainOrder(t *testing.T) {
	t.Parallel()

	var order []string
	mk := func(n string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, n)
				next.ServeHTTP(w, r)
			})
		}
	}
	Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { order = append(order, "h") }), mk("a"), mk("b")).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if strings.Join(order, ",") != "a,b,h" {
		t.Fatalf("order %v", order)
	}
}
