package auth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres/postgrestest"
)

type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type sessionEnv struct {
	pool     *pgxpool.Pool
	sessions *Sessions
	clock    *testClock
	userID   string
}

func newSessionEnv(t *testing.T) *sessionEnv {
	t.Helper()
	pool := postgrestest.New(t)
	clock := &testClock{t: time.Now().UTC().Truncate(time.Second)}
	tk := newTestTokens(t, clock.Now, testAccessKey, testRefreshKey)
	var userID string
	err := pool.QueryRow(context.Background(),
		`INSERT INTO admin_users (email, password_hash) VALUES ('admin@example.com', 'x') RETURNING id::text`).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &sessionEnv{pool: pool, sessions: NewSessions(NewStore(pool), tk, log), clock: clock, userID: userID}
}

type tokenRow struct {
	id         string
	revoked    bool
	replacedBy *string
}

func (e *sessionEnv) rows(t *testing.T) []tokenRow {
	t.Helper()
	r, err := e.pool.Query(context.Background(),
		`SELECT id::text, revoked_at IS NOT NULL, replaced_by_id::text FROM admin_refresh_tokens ORDER BY created_at, id`)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var out []tokenRow
	for r.Next() {
		var tr tokenRow
		if err := r.Scan(&tr.id, &tr.revoked, &tr.replacedBy); err != nil {
			t.Fatal(err)
		}
		out = append(out, tr)
	}
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSessionsRefreshRotates(t *testing.T) {
	e := newSessionEnv(t)
	ctx := context.Background()
	first, err := e.sessions.Issue(ctx, e.userID, ClientMeta{IP: "1.2.3.4", UserAgent: "test"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.sessions.Refresh(ctx, first.RefreshToken.Reveal(), ClientMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("refresh token not rotated")
	}
	rows := e.rows(t)
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	var old, fresh *tokenRow
	for i := range rows {
		if rows[i].replacedBy != nil {
			old = &rows[i]
		} else {
			fresh = &rows[i]
		}
	}
	if old == nil || fresh == nil || !old.revoked || fresh.revoked || *old.replacedBy != fresh.id {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}

func TestSessionsRefreshGrace(t *testing.T) {
	e := newSessionEnv(t)
	ctx := context.Background()
	first, err := e.sessions.Issue(ctx, e.userID, ClientMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.sessions.Refresh(ctx, first.RefreshToken.Reveal(), ClientMeta{}); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(5 * time.Minute)
	if _, err := e.sessions.Refresh(ctx, first.RefreshToken.Reveal(), ClientMeta{}); err != nil {
		t.Fatalf("grace refresh: %v", err)
	}
	live := 0
	for _, r := range e.rows(t) {
		if !r.revoked {
			live++
		}
	}
	if live != 2 {
		t.Fatalf("live rows = %d, want 2", live)
	}
}

func TestSessionsRefreshReplay(t *testing.T) {
	e := newSessionEnv(t)
	ctx := context.Background()
	first, err := e.sessions.Issue(ctx, e.userID, ClientMeta{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.sessions.Refresh(ctx, first.RefreshToken.Reveal(), ClientMeta{})
	if err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(time.Minute)
	if _, err := e.sessions.Refresh(ctx, second.RefreshToken.Reveal(), ClientMeta{}); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(11 * time.Minute)
	if _, err := e.sessions.Refresh(ctx, first.RefreshToken.Reveal(), ClientMeta{}); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("got %v", err)
	}
	for _, r := range e.rows(t) {
		if !r.revoked {
			t.Fatalf("row %s still live", r.id)
		}
	}
	var set bool
	if err := e.pool.QueryRow(ctx, `SELECT sessions_revoked_at IS NOT NULL FROM admin_users WHERE id = $1`, e.userID).Scan(&set); err != nil || !set {
		t.Fatalf("sessions_revoked_at set = %v err = %v", set, err)
	}
}

func TestSessionsRefreshConcurrent(t *testing.T) {
	e := newSessionEnv(t)
	ctx := context.Background()
	first, err := e.sessions.Issue(ctx, e.userID, ClientMeta{})
	if err != nil {
		t.Fatal(err)
	}
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = e.sessions.Refresh(ctx, first.RefreshToken.Reveal(), ClientMeta{})
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("refresh: %v", err)
		}
	}
	rotated := 0
	for _, r := range e.rows(t) {
		if r.replacedBy != nil {
			rotated++
		}
	}
	if rotated != 1 {
		t.Fatalf("rotated rows = %d, want 1", rotated)
	}
}

func TestSessionsAuthenticate(t *testing.T) {
	e := newSessionEnv(t)
	ctx := context.Background()
	s, err := e.sessions.Issue(ctx, e.userID, ClientMeta{})
	if err != nil {
		t.Fatal(err)
	}
	p, err := e.sessions.Authenticate(ctx, s.AccessToken.Reveal())
	if err != nil || p.UserID != e.userID {
		t.Fatalf("authenticate: %v %+v", err, p)
	}
	e.clock.Advance(5 * time.Second)
	if err := e.sessions.RevokeAll(ctx, e.userID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.sessions.Authenticate(ctx, s.AccessToken.Reveal()); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("old access: %v", err)
	}
	e.clock.Advance(5 * time.Second)
	fresh, err := e.sessions.Issue(ctx, e.userID, ClientMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.sessions.Authenticate(ctx, fresh.AccessToken.Reveal()); err != nil {
		t.Fatalf("new access: %v", err)
	}
}

func TestSessionsLogout(t *testing.T) {
	e := newSessionEnv(t)
	ctx := context.Background()
	s, err := e.sessions.Issue(ctx, e.userID, ClientMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.sessions.Logout(ctx, s.RefreshToken.Reveal()); err != nil {
		t.Fatal(err)
	}
	rows := e.rows(t)
	if len(rows) != 1 || !rows[0].revoked {
		t.Fatalf("rows: %+v", rows)
	}
	if err := e.sessions.Logout(ctx, "garbage"); err != nil {
		t.Fatalf("garbage: %v", err)
	}
	if _, err := e.sessions.Refresh(ctx, s.RefreshToken.Reveal(), ClientMeta{}); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("refresh after logout: %v", err)
	}
}

func TestSessionsLogoutThenGraceRefresh(t *testing.T) {
	e := newSessionEnv(t)
	ctx := context.Background()
	first, err := e.sessions.Issue(ctx, e.userID, ClientMeta{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := e.sessions.Refresh(ctx, first.RefreshToken.Reveal(), ClientMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.sessions.Logout(ctx, second.RefreshToken.Reveal()); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(time.Minute)
	if _, err := e.sessions.Refresh(ctx, first.RefreshToken.Reveal(), ClientMeta{}); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestSessionsLoginAfterRevokeAll(t *testing.T) {
	e := newSessionEnv(t)
	ctx := context.Background()
	if _, err := e.sessions.Issue(ctx, e.userID, ClientMeta{}); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(time.Second)
	if err := e.sessions.RevokeAll(ctx, e.userID); err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(time.Second)
	s, err := e.sessions.Issue(ctx, e.userID, ClientMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.sessions.Refresh(ctx, s.RefreshToken.Reveal(), ClientMeta{}); err != nil {
		t.Fatalf("first refresh after login: %v", err)
	}
}

func TestSessionsRevokeAllConcurrentWithRefresh(t *testing.T) {
	e := newSessionEnv(t)
	ctx := context.Background()
	first, err := e.sessions.Issue(ctx, e.userID, ClientMeta{})
	if err != nil {
		t.Fatal(err)
	}
	e.clock.Advance(time.Second)
	const refreshers = 4
	errs := make([]error, refreshers+1)
	var wg sync.WaitGroup
	for i := range refreshers {
		wg.Go(func() {
			_, errs[i] = e.sessions.Refresh(ctx, first.RefreshToken.Reveal(), ClientMeta{})
		})
	}
	wg.Go(func() { errs[refreshers] = e.sessions.RevokeAll(ctx, e.userID) })
	wg.Wait()
	if errs[refreshers] != nil {
		t.Fatalf("revoke all: %v", errs[refreshers])
	}
	for _, err := range errs[:refreshers] {
		if err != nil && !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("refresh: %v", err)
		}
	}
	for _, r := range e.rows(t) {
		if !r.revoked {
			t.Fatalf("row %s survived revoke all", r.id)
		}
	}
}
