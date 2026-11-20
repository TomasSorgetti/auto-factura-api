package auth

import (
	"context"
	"log/slog"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

func TestPruneTokensWorker(t *testing.T) {
	t.Parallel()
	a, pool, _ := newAccounts(t)
	ctx := context.Background()

	w := &PruneTokensWorker{Accounts: a, Log: slog.New(slog.DiscardHandler)}
	job := &river.Job[PruneTokensArgs]{JobRow: &rivertype.JobRow{}}
	if err := w.Work(ctx, job); err != nil {
		t.Fatalf("without admin: %v", err)
	}

	id, err := a.CreateUser(ctx, "vos@ejemplo.com", "una-contraseña-larga")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, insertRefresh, id, "aa", "-1 hour"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, insertRefresh, id, "bb", "1 hour"); err != nil {
		t.Fatal(err)
	}

	if err := w.Work(ctx, job); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM admin_refresh_tokens`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("remaining = %d err=%v", n, err)
	}
}
