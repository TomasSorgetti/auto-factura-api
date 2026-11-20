package httpx

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
)

type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

type Surface string

const (
	SurfaceSystem Surface = "system"
	SurfaceAdmin  Surface = "admin"
)

type Route struct {
	Method  string
	Path    string
	Surface Surface
}

type RouterConfig struct {
	Errors       *ErrorMapper
	RequireAdmin Middleware
}

// Router owns a private ServeMux, so every route goes through System or
// Admin. Admin routes are always wrapped in RequireAdmin.
type Router struct {
	mux    *http.ServeMux
	errors *ErrorMapper
	admin  Middleware
	routes []Route

	surfaces map[string]Surface
}

func NewRouter(cfg RouterConfig) (*Router, error) {
	if cfg.Errors == nil {
		return nil, errors.New("router: error mapper is required")
	}
	if cfg.RequireAdmin == nil {
		return nil, errors.New("router: admin middleware is required")
	}
	return &Router{
		mux:      http.NewServeMux(),
		errors:   cfg.Errors,
		admin:    cfg.RequireAdmin,
		surfaces: make(map[string]Surface),
	}, nil
}

// System registers a route outside the admin and public surfaces. It panics
// on an invalid pattern, like ServeMux.Handle.
func (rt *Router) System(pattern string, h HandlerFunc) {
	method, path := splitPattern(pattern)
	if strings.HasPrefix(path, "/api/admin/") || strings.HasPrefix(path, "/api/public/") {
		panic(fmt.Sprintf("httpx: system route %q must not be under /api/admin/ or /api/public/", pattern))
	}
	rt.register(pattern, method, path, SurfaceSystem, rt.adapt(h))
}

// Admin registers a route under /api/admin/ behind RequireAdmin.
func (rt *Router) Admin(pattern string, h HandlerFunc) {
	method, path := splitPattern(pattern)
	if !strings.HasPrefix(path, "/api/admin/") {
		panic(fmt.Sprintf("httpx: admin route %q must be under /api/admin/", pattern))
	}
	rt.register(pattern, method, path, SurfaceAdmin, rt.admin(rt.adapt(h)))
}

func (rt *Router) register(pattern, method, path string, s Surface, h http.Handler) {
	rt.mux.Handle(pattern, h)
	rt.surfaces[pattern] = s
	rt.routes = append(rt.routes, Route{Method: method, Path: path, Surface: s})
}

// Routes returns a copy of the registered routes sorted by path and method.
func (rt *Router) Routes() []Route {
	out := slices.Clone(rt.routes)
	slices.SortFunc(out, func(a, b Route) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		return strings.Compare(a.Method, b.Method)
	})
	return out
}

func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h, pattern := rt.mux.Handler(r)
	if pattern == "" {
		probe := &discardWriter{header: http.Header{}}
		h.ServeHTTP(probe, r)
		if probe.status == http.StatusMethodNotAllowed {
			if allow := probe.header.Get("Allow"); allow != "" {
				w.Header().Set("Allow", allow)
			}
			rt.errors.Write(w, r, ErrMethodNotAllowed)
			return
		}
		rt.errors.Write(w, r, ErrNotFound)
		return
	}
	if info := requestInfoFrom(r.Context()); info != nil {
		info.pattern = pattern
	}
	if strings.HasPrefix(r.URL.Path, "/api/admin/") && rt.surfaces[pattern] != SurfaceAdmin {
		rt.errors.Write(w, r, ErrNotFound)
		return
	}
	rt.mux.ServeHTTP(w, r)
}

func (rt *Router) adapt(h HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw, ok := w.(*statusWriter)
		if !ok {
			sw = &statusWriter{ResponseWriter: w}
		}
		err := h(sw, r)
		if err == nil {
			return
		}
		if sw.wroteHeader() {
			rt.errors.log.WarnContext(r.Context(), "error after response started", "err", err)
			return
		}
		rt.errors.Write(sw, r, err)
	})
}

type discardWriter struct {
	header http.Header
	status int
}

func (d *discardWriter) Header() http.Header { return d.header }

func (d *discardWriter) WriteHeader(code int) {
	if d.status == 0 {
		d.status = code
	}
}

func (d *discardWriter) Write(b []byte) (int, error) {
	if d.status == 0 {
		d.status = http.StatusOK
	}
	return len(b), nil
}

func splitPattern(pattern string) (method, path string) {
	method, path, ok := strings.Cut(pattern, " ")
	if !ok || !slices.Contains([]string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}, method) ||
		!strings.HasPrefix(path, "/") {
		panic(fmt.Sprintf("httpx: route pattern %q must be \"METHOD /path\" with GET, POST, PUT, PATCH or DELETE", pattern))
	}
	return method, path
}
