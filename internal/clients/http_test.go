package clients

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TomasSorgetti/auto-factura-api/internal/auth"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/httpx"
)

const httpZeroUUID = "00000000-0000-0000-0000-000000000000"

func testErrors() *httpx.ErrorMapper {
	return httpx.NewErrorMapper(slog.New(slog.NewTextHandler(io.Discard, nil)), []httpx.ErrorRule{
		{Target: ErrNotFound, Status: http.StatusNotFound, Code: "NOT_FOUND", Message: "resource not found"},
		{Target: ErrCUITTaken, Status: http.StatusConflict, Code: "CLIENT_CUIT_TAKEN", Message: "a client with this cuit already exists"},
		{Target: ErrArchived, Status: http.StatusConflict, Code: "CLIENT_ARCHIVED", Message: "client is archived"},
		{Target: ErrEmailTaken, Status: http.StatusConflict, Code: "CONTACT_EMAIL_TAKEN", Message: "contact email already exists for this client"},
		{Target: ErrContactLimit, Status: http.StatusConflict, Code: "CONTACT_LIMIT_REACHED", Message: "client contact limit reached"},
	})
}

func newRouter(t *testing.T, svc *Service, principal *auth.Principal) *httpx.Router {
	t.Helper()
	errs := testErrors()
	rt, err := httpx.NewRouter(httpx.RouterConfig{
		Errors: errs,
		RequireAdmin: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if principal == nil {
					errs.Write(w, r, httpx.ErrUnauthenticated)
					return
				}
				next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), *principal)))
			})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	NewHandler(svc).Register(rt)
	return rt
}

func doJSON(rt http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	rt.ServeHTTP(rec, req)
	return rec
}

func respCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var b struct {
		Error struct {
			Code   string            `json:"code"`
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	return b.Error.Code
}

func decodeData[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var b struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	return b.Data
}

func clientBody(seq int, name string) string {
	b, _ := json.Marshal(ClientInput{LegalName: name, CUIT: cuitFor(seq), TaxCondition: "monotributo", Address: "Calle 1", Notes: "nota secreta " + name})
	return string(b)
}

func contactBody(email string) string {
	b, _ := json.Marshal(ContactInput{Name: "Persona Privada", Email: email, Role: "to"})
	return string(b)
}

func httpEnv(t *testing.T) (env, *httpx.Router) {
	t.Helper()
	e := newEnv(t)
	return e, newRouter(t, e.svc, &auth.Principal{UserID: e.actor.ID})
}

func TestClientsHTTPUnauthenticated(t *testing.T) {
	t.Parallel()

	rt := newRouter(t, nil, nil)
	id := httpZeroUUID
	for _, route := range rt.Routes() {
		path := strings.NewReplacer("{id}", id, "{contactId}", id).Replace(route.Path)
		rec := doJSON(rt, route.Method, path, "")
		if rec.Code != http.StatusUnauthorized || respCode(t, rec) != "UNAUTHENTICATED" {
			t.Errorf("%s %s: status %d body %s", route.Method, route.Path, rec.Code, rec.Body)
		}
	}
	if n := len(rt.Routes()); n != 10 {
		t.Fatalf("routes = %d, want 10", n)
	}
}

func TestClientsHTTPListHasNoPII(t *testing.T) {
	t.Parallel()

	_, rt := httpEnv(t)
	rec := doJSON(rt, "POST", "/api/admin/clients", clientBody(1, "Acme SA"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	id := decodeData[detailDTO](t, rec).ID
	if rec := doJSON(rt, "POST", "/api/admin/clients/"+id+"/contacts", contactBody("privado@example.com")); rec.Code != http.StatusCreated {
		t.Fatalf("contact: %d %s", rec.Code, rec.Body)
	}

	rec = doJSON(rt, "GET", "/api/admin/clients", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	for _, banned := range []string{"email", "notes", `"name"`, "privado@example.com", "nota secreta", "Persona Privada"} {
		if strings.Contains(body, banned) {
			t.Errorf("list body contains %q: %s", banned, body)
		}
	}
	list := decodeData[listDTO](t, rec)
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].ContactsCount != 1 || list.Limit != DefaultLimit {
		t.Fatalf("list = %+v", list)
	}
}

func TestClientsHTTPCrossClient(t *testing.T) {
	t.Parallel()

	e, rt := httpEnv(t)
	a := e.client(t, 1, "A")
	b := e.client(t, 2, "B")
	c := e.contact(t, a.ID, "a@example.com")

	path := "/api/admin/clients/" + b.ID + "/contacts/" + c.ID
	if rec := doJSON(rt, "PUT", path, contactBody("x@example.com")); rec.Code != http.StatusNotFound {
		t.Errorf("PUT cross-client: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(rt, "DELETE", path, ""); rec.Code != http.StatusNotFound {
		t.Errorf("DELETE cross-client: %d %s", rec.Code, rec.Body)
	}
	if rec := doJSON(rt, "GET", "/api/admin/clients/not-a-uuid", ""); rec.Code != http.StatusNotFound || respCode(t, rec) != "NOT_FOUND" {
		t.Errorf("GET invalid id: %d %s", rec.Code, rec.Body)
	}
}

func TestClientsHTTPErrors(t *testing.T) {
	t.Parallel()

	_, rt := httpEnv(t)
	first := clientBody(1, "Acme SA")
	rec := doJSON(rt, "POST", "/api/admin/clients", first)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	id := decodeData[detailDTO](t, rec).ID

	rec = doJSON(rt, "POST", "/api/admin/clients", clientBody(1, "Otro"))
	if rec.Code != http.StatusConflict || respCode(t, rec) != "CLIENT_CUIT_TAKEN" || strings.Contains(rec.Body.String(), cuitFor(1)) {
		t.Errorf("duplicate cuit: %d %s", rec.Code, rec.Body)
	}

	if rec := doJSON(rt, "POST", "/api/admin/clients/"+id+"/contacts", contactBody("dup@example.com")); rec.Code != http.StatusCreated {
		t.Fatalf("contact: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(rt, "POST", "/api/admin/clients/"+id+"/contacts", contactBody("dup@example.com"))
	if rec.Code != http.StatusConflict || respCode(t, rec) != "CONTACT_EMAIL_TAKEN" || strings.Contains(rec.Body.String(), "dup@example.com") {
		t.Errorf("duplicate email: %d %s", rec.Code, rec.Body)
	}

	rec = doJSON(rt, "POST", "/api/admin/clients", `{"legalName":"X","bogus":1}`)
	if rec.Code != http.StatusBadRequest || respCode(t, rec) != "INVALID_JSON" {
		t.Errorf("unknown field: %d %s", rec.Code, rec.Body)
	}

	rec = doJSON(rt, "POST", "/api/admin/clients", `{"legalName":"","cuit":"123","taxCondition":"zzz","address":"a"}`)
	if rec.Code != http.StatusUnprocessableEntity || respCode(t, rec) != "VALIDATION_FAILED" {
		t.Fatalf("validation: %d %s", rec.Code, rec.Body)
	}
	if body := rec.Body.String(); strings.Contains(body, "zzz") || strings.Contains(body, `"123"`) {
		t.Errorf("validation echoes values: %s", body)
	}
	var fe struct {
		Error struct {
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &fe); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"legalName", "cuit", "taxCondition"} {
		if fe.Error.Fields[f] == "" {
			t.Errorf("missing field error %q: %v", f, fe.Error.Fields)
		}
	}

	for _, q := range []string{"status=foo", "limit=abc", "offset=abc"} {
		if rec := doJSON(rt, "GET", "/api/admin/clients?"+q, ""); rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d %s", q, rec.Code, rec.Body)
		}
	}
}

func TestClientsHTTPFlow(t *testing.T) {
	t.Parallel()

	_, rt := httpEnv(t)
	rec := doJSON(rt, "POST", "/api/admin/clients", clientBody(1, "Acme SA"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	id := decodeData[detailDTO](t, rec).ID
	base := "/api/admin/clients/" + id

	rec = doJSON(rt, "POST", base+"/contacts", contactBody("uno@example.com"))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create contact: %d %s", rec.Code, rec.Body)
	}
	contactID := decodeData[contactDTO](t, rec).ID

	d := decodeData[detailDTO](t, doJSON(rt, "GET", base, ""))
	if len(d.Contacts) != 1 || d.Contacts[0].Email != "uno@example.com" || d.Notes == "" || d.ArchivedAt != nil {
		t.Fatalf("detail = %+v", d)
	}

	rec = doJSON(rt, "PUT", base+"/contacts/"+contactID, contactBody("dos@example.com"))
	if rec.Code != http.StatusOK || decodeData[contactDTO](t, rec).Email != "dos@example.com" {
		t.Fatalf("update contact: %d %s", rec.Code, rec.Body)
	}

	rec = doJSON(rt, "POST", base+"/archive", "")
	if rec.Code != http.StatusOK || decodeData[detailDTO](t, rec).ArchivedAt == nil {
		t.Fatalf("archive: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(rt, "PUT", base, clientBody(1, "Nuevo nombre"))
	if rec.Code != http.StatusConflict || respCode(t, rec) != "CLIENT_ARCHIVED" {
		t.Fatalf("update archived: %d %s", rec.Code, rec.Body)
	}
	if list := decodeData[listDTO](t, doJSON(rt, "GET", "/api/admin/clients?status=archived", "")); list.Total != 1 {
		t.Fatalf("archived list = %+v", list)
	}

	rec = doJSON(rt, "POST", base+"/unarchive", "")
	if rec.Code != http.StatusOK || decodeData[detailDTO](t, rec).ArchivedAt != nil {
		t.Fatalf("unarchive: %d %s", rec.Code, rec.Body)
	}
	rec = doJSON(rt, "PUT", base, clientBody(1, "Nuevo nombre"))
	if rec.Code != http.StatusOK || decodeData[detailDTO](t, rec).LegalName != "Nuevo nombre" {
		t.Fatalf("update: %d %s", rec.Code, rec.Body)
	}

	rec = doJSON(rt, "DELETE", base+"/contacts/"+contactID, "")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("delete contact: %d %s", rec.Code, rec.Body)
	}
	if d := decodeData[detailDTO](t, doJSON(rt, "GET", base, "")); len(d.Contacts) != 0 {
		t.Fatalf("contacts after delete = %+v", d.Contacts)
	}

	rec = doJSON(rt, "DELETE", base, "")
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("delete client: %d %s", rec.Code, rec.Body)
	}
	for _, method := range []string{"GET", "DELETE"} {
		if rec = doJSON(rt, method, base, ""); rec.Code != http.StatusNotFound {
			t.Fatalf("%s deleted client: %d %s", method, rec.Code, rec.Body)
		}
	}
	if rec = doJSON(rt, "DELETE", "/api/admin/clients/not-a-uuid", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("delete invalid id: %d %s", rec.Code, rec.Body)
	}
}

func TestClientsHTTPBodyLimit(t *testing.T) {
	t.Parallel()

	_, rt := httpEnv(t)
	body := `{"legalName":"` + strings.Repeat("a", 65<<10) + `"}`
	rec := doJSON(rt, "POST", "/api/admin/clients", body)
	if rec.Code != http.StatusRequestEntityTooLarge || respCode(t, rec) != "PAYLOAD_TOO_LARGE" {
		t.Fatalf("oversized body: %d %.200s", rec.Code, rec.Body)
	}
}
