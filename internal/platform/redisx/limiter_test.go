package redisx

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
)

func newTestLimiter(t *testing.T) (*Limiter, *redis.Client) {
	t.Helper()
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		t.Skip("TEST_REDIS_URL is not set")
	}
	rdb, err := New(context.Background(), logx.Secret(url))
	if err != nil {
		t.Fatal(err)
	}
	prefix := "rltest:" + strings.ReplaceAll(t.Name(), "/", "_") + ":" + time.Now().Format("150405.000000000")
	t.Cleanup(func() {
		ctx := context.Background()
		iter := rdb.Scan(ctx, 0, prefix+"*", 100).Iterator()
		for iter.Next(ctx) {
			rdb.Del(ctx, iter.Val())
		}
		_ = rdb.Close()
	})
	l := NewLimiter(rdb, LimiterConfig{Prefix: prefix, MaxFailures: 5, Window: 15 * time.Minute, MaxBlock: 24 * time.Hour})
	return l, rdb
}

func approx(t *testing.T, got, want time.Duration) {
	t.Helper()
	if got < want-time.Minute || got > want {
		t.Fatalf("duration = %v, want about %v", got, want)
	}
}

func takeN(t *testing.T, l *Limiter, n int, keys ...string) time.Duration {
	t.Helper()
	var last time.Duration
	for i := 0; i < n; i++ {
		d, err := l.Take(context.Background(), keys...)
		if err != nil {
			t.Fatal(err)
		}
		last = d
	}
	return last
}

func TestLimiter(t *testing.T) {
	ctx := context.Background()

	t.Run("blocks on the sixth attempt", func(t *testing.T) {
		t.Parallel()
		l, _ := newTestLimiter(t)
		if d := takeN(t, l, 5, "a"); d != 0 {
			t.Fatalf("blocked within limit: %v", d)
		}
		approx(t, takeN(t, l, 1, "a"), 15*time.Minute)
		approx(t, takeN(t, l, 1, "a"), 15*time.Minute)
	})

	t.Run("second block doubles", func(t *testing.T) {
		t.Parallel()
		l, rdb := newTestLimiter(t)
		takeN(t, l, 6, "a")
		_, block, _ := l.keys("a")
		if err := rdb.Del(ctx, block).Err(); err != nil {
			t.Fatal(err)
		}
		if d := takeN(t, l, 5, "a"); d != 0 {
			t.Fatalf("blocked within limit: %v", d)
		}
		approx(t, takeN(t, l, 1, "a"), 30*time.Minute)
	})

	t.Run("reset clears attempts and strikes", func(t *testing.T) {
		t.Parallel()
		l, rdb := newTestLimiter(t)
		takeN(t, l, 6, "a")
		if err := l.Reset(ctx, "a"); err != nil {
			t.Fatal(err)
		}
		fails, _, strikes := l.keys("a")
		n, err := rdb.Exists(ctx, fails, strikes).Result()
		if err != nil || n != 0 {
			t.Fatalf("exists = %d, %v", n, err)
		}
	})

	t.Run("one blocked key is enough", func(t *testing.T) {
		t.Parallel()
		l, _ := newTestLimiter(t)
		takeN(t, l, 6, "b")
		if d := takeN(t, l, 1, "a", "b"); d <= 0 {
			t.Fatal("expected blocked")
		}
	})

	t.Run("concurrent attempts never exceed the limit", func(t *testing.T) {
		t.Parallel()
		l, _ := newTestLimiter(t)
		const workers = 40
		var allowed atomic.Int64
		var wg sync.WaitGroup
		for i := 0; i < workers; i++ {
			wg.Go(func() {
				d, err := l.Take(ctx, "a")
				if err != nil {
					t.Error(err)
					return
				}
				if d == 0 {
					allowed.Add(1)
				}
			})
		}
		wg.Wait()
		if got := allowed.Load(); got != 5 {
			t.Fatalf("allowed = %d, want 5", got)
		}
	})
}
