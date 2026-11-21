package clients

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TomasSorgetti/auto-factura-api/internal/audit"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres/postgrestest"
)

func cuitFor(seq int) string {
	weights := [10]int{5, 4, 3, 2, 7, 6, 5, 4, 3, 2}
	base := fmt.Sprintf("20%08d", seq)
	sum := 0
	for i, w := range weights {
		sum += int(base[i]-'0') * w
	}
	check := 11 - sum%11
	if check == 11 {
		check = 0
	}
	if check == 10 {
		return cuitFor(seq + 1000003)
	}
	return base + fmt.Sprint(check)
}

type env struct {
	pool  *pgxpool.Pool
	svc   *Service
	actor audit.Actor
}

func newEnv(t *testing.T) env {
	t.Helper()
	pool := postgrestest.New(t)
	var adminID string
	err := pool.QueryRow(context.Background(),
		"INSERT INTO admin_users (email, password_hash) VALUES ('admin@example.com', 'x') RETURNING id::text").Scan(&adminID)
	if err != nil {
		t.Fatal(err)
	}
	return env{
		pool:  pool,
		svc:   NewService(pool, newTestCodec(t)),
		actor: audit.Actor{Type: audit.ActorAdmin, ID: adminID},
	}
}

func (e env) client(t *testing.T, seq int, name string) Detail {
	t.Helper()
	d, err := e.svc.Create(context.Background(), e.actor, ClientInput{
		LegalName: name, CUIT: cuitFor(seq), TaxCondition: "monotributo", Address: "Calle 1", Notes: "nota " + name,
	})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func (e env) contact(t *testing.T, clientID, email string) Contact {
	t.Helper()
	c, err := e.svc.CreateContact(context.Background(), e.actor, clientID, ContactInput{Name: "Persona", Email: email, Role: "to"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (e env) events(t *testing.T, action string) []string {
	t.Helper()
	rows, err := e.pool.Query(context.Background(),
		"SELECT data::text FROM audit_events WHERE action = $1 ORDER BY occurred_at, id", action)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatal(err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestServiceEncryptsAtRest(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	d, err := e.svc.Create(ctx, e.actor, ClientInput{
		LegalName: "Acme SA", CUIT: cuitFor(1), TaxCondition: "registered", Address: "Calle 1", Notes: "notas muy privadas",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.CreateContact(ctx, e.actor, d.ID, ContactInput{Name: "Beatriz Nombre", Email: "beatriz@privado.example", Role: "cc"}); err != nil {
		t.Fatal(err)
	}
	var notes, name, email []byte
	if err := e.pool.QueryRow(ctx, "SELECT notes_enc FROM clients").Scan(&notes); err != nil {
		t.Fatal(err)
	}
	if err := e.pool.QueryRow(ctx, "SELECT name_enc, email_enc FROM client_contacts").Scan(&name, &email); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		blob  []byte
		plain string
	}{{notes, "notas muy privadas"}, {name, "Beatriz Nombre"}, {email, "beatriz@privado.example"}} {
		if bytes.Contains(c.blob, []byte(c.plain)) {
			t.Errorf("ciphertext contains %q", c.plain)
		}
	}
	got, err := e.svc.Get(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Notes != "notas muy privadas" || got.Contacts[0].Email != "beatriz@privado.example" || got.Contacts[0].Name != "Beatriz Nombre" {
		t.Fatalf("detail = %+v", got)
	}
}

func TestServiceSwappedCiphertext(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	d := e.client(t, 1, "Acme")
	a := e.contact(t, d.ID, "a@example.com")
	b := e.contact(t, d.ID, "b@example.com")
	if _, err := e.pool.Exec(ctx,
		"UPDATE client_contacts SET email_enc = (SELECT email_enc FROM client_contacts WHERE id = $1) WHERE id = $2", a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.Get(ctx, d.ID)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "a@example.com") || strings.Contains(err.Error(), "b@example.com") {
		t.Fatalf("error leaks email: %v", err)
	}
}

func TestServiceCRUD(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	d := e.client(t, 1, "Acme")
	if d.ArchivedAt != nil || d.Notes != "nota Acme" || len(d.Contacts) != 0 {
		t.Fatalf("created = %+v", d)
	}

	in := ClientInput{LegalName: "Acme 2", CUIT: cuitFor(1), TaxCondition: "monotributo", Address: "Calle 1", Notes: "nota Acme"}
	up, err := e.svc.Update(ctx, e.actor, d.ID, in)
	if err != nil || up.LegalName != "Acme 2" {
		t.Fatalf("update = %+v, %v", up, err)
	}
	if ev := e.events(t, "client.updated"); len(ev) != 1 || !strings.Contains(ev[0], `"fields": "legalName"`) {
		t.Fatalf("events = %v", ev)
	}
	if _, err := e.svc.Update(ctx, e.actor, d.ID, in); err != nil {
		t.Fatal(err)
	}
	if ev := e.events(t, "client.updated"); len(ev) != 1 {
		t.Fatalf("no-op update wrote an event: %v", ev)
	}
	in.Notes = ""
	cleared, err := e.svc.Update(ctx, e.actor, d.ID, in)
	if err != nil || cleared.Notes != "" {
		t.Fatalf("clear notes = %+v, %v", cleared, err)
	}

	arch, err := e.svc.Archive(ctx, e.actor, d.ID)
	if err != nil || arch.ArchivedAt == nil {
		t.Fatalf("archive = %+v, %v", arch, err)
	}
	if _, err := e.svc.Archive(ctx, e.actor, d.ID); err != nil {
		t.Fatal(err)
	}
	if ev := e.events(t, "client.archived"); len(ev) != 1 {
		t.Fatalf("archive events = %v", ev)
	}
	un, err := e.svc.Unarchive(ctx, e.actor, d.ID)
	if err != nil || un.ArchivedAt != nil {
		t.Fatalf("unarchive = %+v, %v", un, err)
	}
	if _, err := e.svc.Unarchive(ctx, e.actor, d.ID); err != nil {
		t.Fatal(err)
	}
	if ev := e.events(t, "client.unarchived"); len(ev) != 1 {
		t.Fatalf("unarchive events = %v", ev)
	}

	c := e.contact(t, d.ID, "Ana@Ejemplo.com")
	if c.Email != "ana@ejemplo.com" {
		t.Fatalf("email = %q", c.Email)
	}
	cu, err := e.svc.UpdateContact(ctx, e.actor, d.ID, c.ID, ContactInput{Name: "Persona", Email: "ana@ejemplo.com", Role: "cc", PortalAccess: true})
	if err != nil || cu.Role != RoleCc || !cu.PortalAccess {
		t.Fatalf("update contact = %+v, %v", cu, err)
	}
	if ev := e.events(t, "client.contact_updated"); len(ev) != 1 || !strings.Contains(ev[0], "role,portalAccess") {
		t.Fatalf("contact events = %v", ev)
	}
	if err := e.svc.DeleteContact(ctx, e.actor, d.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteContact(ctx, e.actor, d.ID, c.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if ev := e.events(t, "client.contact_deleted"); len(ev) != 1 {
		t.Fatalf("delete events = %v", ev)
	}
}

func TestServiceConflicts(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.client(t, 1, "A")
	b := e.client(t, 2, "B")

	dup := ClientInput{LegalName: "Otro", CUIT: cuitFor(1), TaxCondition: "exempt", Address: "x"}
	if _, err := e.svc.Create(ctx, e.actor, dup); !errors.Is(err, ErrCUITTaken) {
		t.Fatalf("dup cuit: %v", err)
	}
	if _, err := e.svc.Archive(ctx, e.actor, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Create(ctx, e.actor, dup); !errors.Is(err, ErrCUITTaken) {
		t.Fatalf("dup cuit of archived: %v", err)
	}
	upd := ClientInput{LegalName: "B", CUIT: cuitFor(1), TaxCondition: "exempt", Address: "x"}
	if _, err := e.svc.Update(ctx, e.actor, b.ID, upd); !errors.Is(err, ErrCUITTaken) {
		t.Fatalf("update to taken cuit: %v", err)
	}

	e.contact(t, b.ID, "x@example.com")
	_, err := e.svc.CreateContact(ctx, e.actor, b.ID, ContactInput{Name: "Y", Email: "X@Example.com", Role: "cc"})
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("dup email: %v", err)
	}
	if _, err := e.svc.Unarchive(ctx, e.actor, a.ID); err != nil {
		t.Fatal(err)
	}
	e.contact(t, a.ID, "x@example.com")
}

func TestServiceArchivedReadOnly(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	d := e.client(t, 1, "A")
	c := e.contact(t, d.ID, "a@example.com")
	if _, err := e.svc.Archive(ctx, e.actor, d.ID); err != nil {
		t.Fatal(err)
	}
	in := ClientInput{LegalName: "A2", CUIT: cuitFor(1), TaxCondition: "exempt", Address: "x"}
	ci := ContactInput{Name: "N", Email: "n@example.com", Role: "to"}
	if _, err := e.svc.Update(ctx, e.actor, d.ID, in); !errors.Is(err, ErrArchived) {
		t.Errorf("Update: %v", err)
	}
	if _, err := e.svc.CreateContact(ctx, e.actor, d.ID, ci); !errors.Is(err, ErrArchived) {
		t.Errorf("CreateContact: %v", err)
	}
	if _, err := e.svc.UpdateContact(ctx, e.actor, d.ID, c.ID, ci); !errors.Is(err, ErrArchived) {
		t.Errorf("UpdateContact: %v", err)
	}
	if err := e.svc.DeleteContact(ctx, e.actor, d.ID, c.ID); !errors.Is(err, ErrArchived) {
		t.Errorf("DeleteContact: %v", err)
	}
}

func TestServiceCrossClient(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.client(t, 1, "A")
	b := e.client(t, 2, "B")
	c := e.contact(t, a.ID, "a@example.com")
	ci := ContactInput{Name: "Hack", Email: "hack@example.com", Role: "cc"}

	if _, err := e.svc.UpdateContact(ctx, e.actor, b.ID, c.ID, ci); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateContact: %v", err)
	}
	if err := e.svc.DeleteContact(ctx, e.actor, b.ID, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteContact: %v", err)
	}
	got, err := e.svc.Get(ctx, a.ID)
	if err != nil || len(got.Contacts) != 1 || got.Contacts[0].Email != "a@example.com" || got.Contacts[0].Role != RoleTo {
		t.Fatalf("contact changed: %+v, %v", got, err)
	}

	if _, err := e.svc.Get(ctx, "not-a-uuid"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get bad id: %v", err)
	}
	if _, err := e.svc.UpdateContact(ctx, e.actor, a.ID, "nope", ci); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateContact bad contact id: %v", err)
	}
	if err := e.svc.DeleteContact(ctx, e.actor, "nope", c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteContact bad client id: %v", err)
	}
	if _, err := e.svc.CreateContact(ctx, e.actor, "nope", ci); !errors.Is(err, ErrNotFound) {
		t.Errorf("CreateContact bad client id: %v", err)
	}
}

func TestServiceContactLimitRace(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	d := e.client(t, 1, "A")
	for i := range MaxContacts - 1 {
		e.contact(t, d.ID, fmt.Sprintf("c%d@example.com", i))
	}
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = e.svc.CreateContact(ctx, e.actor, d.ID,
				ContactInput{Name: "Race", Email: fmt.Sprintf("race%d@example.com", i), Role: "cc"})
		})
	}
	wg.Wait()
	limited, ok := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrContactLimit):
			limited++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 || limited != 1 {
		t.Fatalf("ok=%d limited=%d", ok, limited)
	}
	got, err := e.svc.Get(ctx, d.ID)
	if err != nil || len(got.Contacts) != MaxContacts {
		t.Fatalf("contacts = %d, %v", len(got.Contacts), err)
	}
}

func TestServiceAuditHasNoPII(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	d, err := e.svc.Create(ctx, e.actor, ClientInput{
		LegalName: "Razon Secreta SA", CUIT: cuitFor(7), TaxCondition: "registered", Address: "Calle Privada 1", Notes: "nota reservada",
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := e.svc.CreateContact(ctx, e.actor, d.ID, ContactInput{Name: "Nombre Secreto", Email: "secreto@privado.example", Role: "to"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.UpdateContact(ctx, e.actor, d.ID, c.ID, ContactInput{Name: "Otro Secreto", Email: "otro@privado.example", Role: "cc"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Update(ctx, e.actor, d.ID, ClientInput{
		LegalName: "Razon Nueva SA", CUIT: cuitFor(7), TaxCondition: "registered", Address: "Calle Privada 1", Notes: "nota nueva",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Archive(ctx, e.actor, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Unarchive(ctx, e.actor, d.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteContact(ctx, e.actor, d.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	var all []string
	for _, a := range []string{"client.created", "client.updated", "client.archived", "client.unarchived",
		"client.contact_created", "client.contact_updated", "client.contact_deleted"} {
		ev := e.events(t, a)
		if len(ev) != 1 {
			t.Fatalf("%s events = %d", a, len(ev))
		}
		all = append(all, ev...)
	}
	joined := strings.Join(all, "\n")
	for _, secret := range []string{"Razon", "Secreta", "Secreto", "privado", "reservada", "nueva", "Privada", cuitFor(7)} {
		if strings.Contains(joined, secret) {
			t.Errorf("audit data contains %q: %s", secret, joined)
		}
	}
}

func TestServiceList(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.client(t, 20123456, "beta")
	e.client(t, 20999999, "Alfa")
	e.client(t, 30111111, "Gamma 50%")
	arch := e.client(t, 40222222, "Delta")
	if _, err := e.svc.Archive(ctx, e.actor, arch.ID); err != nil {
		t.Fatal(err)
	}

	names := func(p Page) []string {
		out := make([]string, 0, len(p.Items))
		for _, it := range p.Items {
			out = append(out, it.LegalName)
		}
		return out
	}
	check := func(label string, p ListParams, want []string, total int) {
		t.Helper()
		got, err := e.svc.List(ctx, p)
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		if strings.Join(names(got), ",") != strings.Join(want, ",") || got.Total != total {
			t.Errorf("%s: got %v total %d", label, names(got), got.Total)
		}
	}
	check("order", ListParams{}, []string{"Alfa", "beta", "Gamma 50%"}, 3)
	check("archived", ListParams{Archived: true}, []string{"Delta"}, 1)
	check("name", ListParams{Query: "ALF"}, []string{"Alfa"}, 1)
	check("cuit prefix", ListParams{Query: cuitFor(20123456)[:6]}, []string{"beta"}, 1)
	check("percent is literal", ListParams{Query: "%"}, []string{"Gamma 50%"}, 1)
	check("underscore is literal", ListParams{Query: "_"}, nil, 0)
	check("offset", ListParams{Limit: 1, Offset: 1}, []string{"beta"}, 3)

	if _, err := e.svc.List(ctx, ListParams{Limit: 1000}); err == nil {
		t.Error("expected validation error")
	}
}

func TestServiceNonCanonicalID(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	d := e.client(t, 1, "A")
	upper := strings.ToUpper(d.ID)
	compact := strings.ReplaceAll(d.ID, "-", "")

	in := ClientInput{LegalName: "A2", CUIT: cuitFor(1), TaxCondition: "monotributo", Address: "Calle 1", Notes: "otra nota"}
	if _, err := e.svc.Update(ctx, e.actor, upper, in); err != nil {
		t.Fatalf("Update upper: %v", err)
	}
	got, err := e.svc.Get(ctx, d.ID)
	if err != nil || got.Notes != "otra nota" {
		t.Fatalf("Get after update: %+v, %v", got, err)
	}
	if _, err := e.svc.Archive(ctx, e.actor, compact); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Unarchive(ctx, e.actor, upper); err != nil {
		t.Fatal(err)
	}
	c, err := e.svc.CreateContact(ctx, e.actor, upper, ContactInput{Name: "N", Email: "n@example.com", Role: "to"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.UpdateContact(ctx, e.actor, compact, strings.ToUpper(c.ID), ContactInput{Name: "N", Email: "n@example.com", Role: "cc"}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.DeleteContact(ctx, e.actor, upper, strings.ToUpper(c.ID)); err != nil {
		t.Fatal(err)
	}

	for _, q := range []string{
		"SELECT count(*) FROM audit_events WHERE action LIKE 'client.%' AND entity_id::text <> lower(entity_id::text)",
	} {
		var n int
		if err := e.pool.QueryRow(ctx, q).Scan(&n); err != nil || n != 0 {
			t.Fatalf("non canonical entity ids: %d, %v", n, err)
		}
	}
	for _, a := range []string{"client.contact_created", "client.contact_updated", "client.contact_deleted"} {
		for _, ev := range e.events(t, a) {
			if !strings.Contains(ev, d.ID) {
				t.Errorf("%s data without canonical client id: %s", a, ev)
			}
		}
	}
}

func TestServiceDelete(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	active := e.client(t, 1, "A")
	e.contact(t, active.ID, "a1@example.com")
	e.contact(t, active.ID, "a2@example.com")
	archived := e.client(t, 2, "B")
	e.contact(t, archived.ID, "b1@example.com")
	if _, err := e.svc.Archive(ctx, e.actor, archived.ID); err != nil {
		t.Fatal(err)
	}
	other := e.client(t, 3, "C")
	e.contact(t, other.ID, "c1@example.com")

	if err := e.svc.Delete(ctx, e.actor, strings.ToUpper(active.ID)); err != nil {
		t.Fatalf("Delete active: %v", err)
	}
	if err := e.svc.Delete(ctx, e.actor, archived.ID); err != nil {
		t.Fatalf("Delete archived: %v", err)
	}
	for _, id := range []string{active.ID, archived.ID} {
		if _, err := e.svc.Get(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get deleted %s: %v", id, err)
		}
		if err := e.svc.Delete(ctx, e.actor, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Delete twice %s: %v", id, err)
		}
	}
	if err := e.svc.Delete(ctx, e.actor, "not-an-id"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete invalid id: %v", err)
	}
	got, err := e.svc.Get(ctx, other.ID)
	if err != nil || len(got.Contacts) != 1 {
		t.Fatalf("other client untouched: %+v, %v", got, err)
	}

	var left int
	if err := e.pool.QueryRow(ctx,
		"SELECT count(*) FROM client_contacts WHERE client_id = ANY($1::uuid[])", []string{active.ID, archived.ID}).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf("contacts left = %d", left)
	}

	events := e.events(t, "client.deleted")
	if len(events) != 2 {
		t.Fatalf("events = %v", events)
	}
	if !strings.Contains(events[0], `"contacts": "2"`) || !strings.Contains(events[1], `"contacts": "1"`) {
		t.Errorf("contact counts = %v", events)
	}
	var ids int
	if err := e.pool.QueryRow(ctx,
		"SELECT count(*) FROM audit_events WHERE action = 'client.deleted' AND entity_id = ANY($1)", []string{active.ID, archived.ID}).Scan(&ids); err != nil {
		t.Fatal(err)
	}
	if ids != 2 {
		t.Errorf("canonical entity ids = %d, want 2", ids)
	}
	for _, ev := range events {
		if strings.Contains(ev, "example.com") || strings.Contains(ev, "Persona") || strings.Contains(ev, cuitFor(1)) {
			t.Errorf("event leaks personal data: %s", ev)
		}
	}
}
