package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errBoom = errors.New("boom")

type failingDB struct{}

func (failingDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errBoom
}

func (failingDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, errBoom }

func (failingDB) QueryRow(context.Context, string, ...any) pgx.Row { return failingRow{} }

type failingRow struct{}

func (failingRow) Scan(...any) error { return errBoom }

func newTestAuditor(pool *pgxpool.Pool) *SecurityAuditor {
	return NewSecurityAuditor(pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

type auditRow struct {
	actorType string
	data      string
	ip        string
}

func auditEvents(t *testing.T, pool *pgxpool.Pool, action string) []auditRow {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT actor_type, data::text, coalesce(host(ip), '') FROM audit_events WHERE action = $1 ORDER BY occurred_at, id`, action)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var r auditRow
		if err := rows.Scan(&r.actorType, &r.data, &r.ip); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
