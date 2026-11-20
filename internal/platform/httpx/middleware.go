package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
)

type Middleware func(http.Handler) http.Handler

// Chain wraps h so that the first middleware is the outermost one.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// Recover turns a panic into a 500 and logs the stack. http.ErrAbortHandler
// is re-panicked.
func Recover(log *slog.Logger, m *ErrorMapper) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sw := &statusWriter{ResponseWriter: w}
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.ErrorContext(r.Context(), "panic", "value", v, "stack", string(debug.Stack()))
				if sw.wroteHeader() {
					panic(http.ErrAbortHandler)
				}
				m.Write(sw, r, ErrInternal)
			}()
			next.ServeHTTP(sw, r)
		})
	}
}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// RequestID propagates a valid X-Request-ID or generates one.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if !requestIDPattern.MatchString(id) {
				var b [16]byte
				_, _ = rand.Read(b[:])
				id = hex.EncodeToString(b[:])
			}
			w.Header().Set("X-Request-ID", id)
			next.ServeHTTP(w, r.WithContext(logx.WithRequestID(r.Context(), id)))
		})
	}
}

// Logger logs one line per request. It never logs the path or query: those
// can carry tokens.
func Logger(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ctx, info := withRequestInfo(r.Context())
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r.WithContext(ctx))

			status := sw.status
			if status == 0 {
				status = http.StatusOK
			}
			level := slog.LevelInfo
			switch {
			case status >= http.StatusInternalServerError:
				level = slog.LevelError
			case status >= http.StatusBadRequest:
				level = slog.LevelWarn
			}
			pattern := info.pattern
			if pattern == "" {
				pattern = "unmatched"
			}
			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("pattern", pattern),
				slog.Int("status", status),
				slog.Int64("bytes", sw.bytes),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			}
			if info.code != "" {
				attrs = append(attrs, slog.String("code", info.code))
			}
			log.LogAttrs(ctx, level, "http request", attrs...)
		})
	}
}

func SecurityHeaders() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Cache-Control", "no-store")
			h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			next.ServeHTTP(w, r)
		})
	}
}

// Timeout sets a deadline on the request context. Handlers must honor it;
// the mapper turns context.DeadlineExceeded into 503.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CrossOrigin rejects cross-origin requests that change state.
func CrossOrigin(trusted []string, m *ErrorMapper) (Middleware, error) {
	p := http.NewCrossOriginProtection()
	for _, o := range trusted {
		if err := p.AddTrustedOrigin(o); err != nil {
			return nil, fmt.Errorf("trusted origin %q: %w", o, err)
		}
	}
	p.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.Write(w, r, ErrCrossOrigin)
	}))
	return p.Handler, nil
}
