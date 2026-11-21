package settings

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TomasSorgetti/auto-factura-api/internal/audit"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres/postgrestest"
)

func newAdmin(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(),
		"INSERT INTO admin_users (email, password_hash) VALUES ('admin@example.com', 'x') RETURNING id::text").Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func auditFields(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		"SELECT data->>'fields' FROM audit_events WHERE action = 'settings.updated' ORDER BY occurred_at, id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			t.Fatal(err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestServiceUpdate(t *testing.T) {
	pool := postgrestest.New(t)
	loc, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(pool, loc)
	ctx := context.Background()
	actor := audit.Actor{Type: audit.ActorAdmin, ID: newAdmin(t, pool), IP: "203.0.113.7"}

	if _, err := svc.Get(ctx); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Get before update: %v", err)
	}

	first, err := svc.Update(ctx, actor, validInput())
	if err != nil {
		t.Fatal(err)
	}
	if first.CUIT != "20123456786" || first.ArcaEnv != ArcaHomologation {
		t.Fatalf("unexpected %+v", first)
	}
	want := "cuit,legalName,address,grossIncomeNumber,activityStartDate,pointOfSale,monotributoCategory,annualCapCents"
	if got := auditFields(t, pool); len(got) != 1 || got[0] != want {
		t.Fatalf("audit after first update: %v", got)
	}

	if _, err := svc.Update(ctx, actor, validInput()); err != nil {
		t.Fatal(err)
	}
	if got := auditFields(t, pool); len(got) != 1 {
		t.Fatalf("identical update wrote an event: %v", got)
	}

	if _, err := pool.Exec(ctx, "UPDATE settings SET arca_env = 'production'"); err != nil {
		t.Fatal(err)
	}
	in := validInput()
	in.PointOfSale = 4
	second, err := svc.Update(ctx, actor, in)
	if err != nil {
		t.Fatal(err)
	}
	if second.PointOfSale != 4 || second.ArcaEnv != ArcaProduction {
		t.Fatalf("arca env must survive an update: %+v", second)
	}
	if got := auditFields(t, pool); len(got) != 2 || got[1] != "pointOfSale" {
		t.Fatalf("audit after second update: %v", got)
	}

	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM settings").Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows = %d, err %v", n, err)
	}
}

func TestServiceUpdateAuditFailureRollsBack(t *testing.T) {
	pool := postgrestest.New(t)
	svc := NewService(pool, time.UTC)
	_, err := svc.Update(context.Background(), audit.Actor{Type: audit.ActorAdmin}, validInput())
	if !errors.Is(err, audit.ErrInvalidEvent) {
		t.Fatalf("got %v", err)
	}
	if _, err := svc.Get(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("row survived a failed audit: %v", err)
	}
}

func arcaEnvEvents(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		"SELECT actor_type || ':' || (data->>'from') || '>' || (data->>'to') FROM audit_events WHERE action = 'settings.arca_env_changed' ORDER BY occurred_at, id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			t.Fatal(err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSetArcaEnv(t *testing.T) {
	pool := postgrestest.New(t)
	svc := NewService(pool, time.UTC)
	ctx := context.Background()
	cli := audit.Actor{Type: audit.ActorCLI}

	if _, err := svc.SetArcaEnv(ctx, ArcaProduction, "20123456786", cli); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("without settings: %v", err)
	}
	if _, err := svc.SetArcaEnv(ctx, ArcaEnv("prod"), "", cli); !errors.Is(err, ErrInvalidArcaEnv) {
		t.Fatalf("invalid env: %v", err)
	}
	if _, err := svc.Update(ctx, audit.Actor{Type: audit.ActorSystem}, validInput()); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.SetArcaEnv(ctx, ArcaProduction, "20-11111111-2", cli); !errors.Is(err, ErrConfirmationMismatch) {
		t.Fatalf("wrong cuit: %v", err)
	}
	if _, err := svc.SetArcaEnv(ctx, ArcaProduction, "", cli); !errors.Is(err, ErrConfirmationMismatch) {
		t.Fatalf("empty cuit: %v", err)
	}
	if cur, err := svc.Get(ctx); err != nil || cur.ArcaEnv != ArcaHomologation {
		t.Fatalf("env changed after a rejected confirmation: %+v, %v", cur, err)
	}
	if got := arcaEnvEvents(t, pool); len(got) != 0 {
		t.Fatalf("events after rejection: %v", got)
	}

	from, err := svc.SetArcaEnv(ctx, ArcaProduction, "20-12345678-6", cli)
	if err != nil || from != ArcaHomologation {
		t.Fatalf("to production: from %q, err %v", from, err)
	}
	if got := arcaEnvEvents(t, pool); len(got) != 1 || got[0] != "cli:homologation>production" {
		t.Fatalf("events: %v", got)
	}

	from, err = svc.SetArcaEnv(ctx, ArcaProduction, "", cli)
	if err != nil || from != ArcaProduction {
		t.Fatalf("repeat: from %q, err %v", from, err)
	}
	if got := arcaEnvEvents(t, pool); len(got) != 1 {
		t.Fatalf("repeat wrote an event: %v", got)
	}

	from, err = svc.SetArcaEnv(ctx, ArcaHomologation, "", cli)
	if err != nil || from != ArcaProduction {
		t.Fatalf("back to homologation: from %q, err %v", from, err)
	}
	if got := arcaEnvEvents(t, pool); len(got) != 2 || got[1] != "cli:production>homologation" {
		t.Fatalf("events: %v", got)
	}
}
