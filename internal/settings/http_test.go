package settings

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/auth"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/httpx"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres/postgrestest"
)

func TestHTTP(t *testing.T) {
	pool := postgrestest.New(t)
	adminID := newAdmin(t, pool)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	errs := httpx.NewErrorMapper(log, nil)

	injectAdmin := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), auth.Principal{UserID: adminID})))
		})
	}
	denyAll := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			errs.Write(w, r, httpx.ErrUnauthenticated)
		})
	}
	newRT := func(mw httpx.Middleware) *httpx.Router {
		rt, err := httpx.NewRouter(httpx.RouterConfig{Errors: errs, RequireAdmin: mw})
		if err != nil {
			t.Fatal(err)
		}
		NewHandler(NewService(pool, time.UTC)).Register(rt)
		return rt
	}
	rt := newRT(injectAdmin)
	do := func(rt http.Handler, method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/admin/settings", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		rt.ServeHTTP(rec, req)
		return rec
	}

	if rec := do(newRT(denyAll), "GET", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no session: %d", rec.Code)
	}

	rec := do(rt, "GET", "")
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"data":null}` {
		t.Fatalf("empty GET: %d %s", rec.Code, rec.Body)
	}

	rec = do(rt, "PUT", `{"cuit":"20123456787","legalName":"J","address":"A","grossIncomeNumber":"1","activityStartDate":"2020-03-01","pointOfSale":3,"monotributoCategory":"D","annualCapCents":100}`)
	var bad struct {
		Error struct {
			Code   string            `json:"code"`
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &bad); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 422 || bad.Error.Code != "VALIDATION_FAILED" || bad.Error.Fields["cuit"] != "invalid cuit" {
		t.Fatalf("invalid PUT: %d %s", rec.Code, rec.Body)
	}

	body := `{"cuit":"20-12345678-6","legalName":"Juan Perez","address":"Av. Siempreviva 742","grossIncomeNumber":"Exento","activityStartDate":"2020-03-01","pointOfSale":3,"monotributoCategory":"D","annualCapCents":2500000000}`
	rec = do(rt, "PUT", body)
	if rec.Code != 200 {
		t.Fatalf("PUT: %d %s", rec.Code, rec.Body)
	}
	var put struct {
		Data response `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &put); err != nil {
		t.Fatal(err)
	}
	if put.Data.ArcaEnv != "homologation" || put.Data.CUIT != "20123456786" || put.Data.ActivityStartDate != "2020-03-01" || put.Data.AnnualCapCents != 2500000000 {
		t.Fatalf("unexpected PUT data %+v", put.Data)
	}

	rec = do(rt, "GET", "")
	var got struct {
		Data response `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Data != put.Data {
		t.Fatalf("GET %s differs from PUT (%v)", rec.Body, err)
	}

	rec = do(rt, "PUT", strings.Replace(body, `"annualCapCents"`, `"arcaEnv":"production","annualCapCents"`, 1))
	if rec.Code != 400 {
		t.Fatalf("arcaEnv in body: %d %s", rec.Code, rec.Body)
	}
}
