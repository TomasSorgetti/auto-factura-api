package redisx

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const strikesTTL = 24 * time.Hour

// LimiterConfig tunes a Limiter. Prefix namespaces every key it writes.
type LimiterConfig struct {
	Prefix      string
	MaxFailures int64
	Window      time.Duration
	MaxBlock    time.Duration
}

// Limiter counts failures per key and blocks a key for a duration that doubles
// with each consecutive block, up to MaxBlock.
type Limiter struct {
	rdb *redis.Client
	cfg LimiterConfig
}

func NewLimiter(rdb *redis.Client, cfg LimiterConfig) *Limiter {
	return &Limiter{rdb: rdb, cfg: cfg}
}

var takeScript = redis.NewScript(`
local ttl = redis.call('TTL', KEYS[2])
if ttl > 0 then return ttl end
local n = redis.call('INCR', KEYS[1])
if n == 1 then redis.call('EXPIRE', KEYS[1], ARGV[2]) end
if n <= tonumber(ARGV[1]) then return 0 end
local s = redis.call('INCR', KEYS[3])
redis.call('EXPIRE', KEYS[3], ARGV[4])
local d = tonumber(ARGV[2]) * 2 ^ math.min(s - 1, 20)
local max = tonumber(ARGV[3])
if d > max then d = max end
d = math.floor(d)
redis.call('SET', KEYS[2], 1, 'EX', d)
redis.call('DEL', KEYS[1])
return d
`)

func (l *Limiter) keys(key string) (fails, block, strikes string) {
	base := fmt.Sprintf("%s:%s", l.cfg.Prefix, key)
	return base + ":fails", base + ":block", base + ":strikes"
}

// Take counts one attempt on every key before it is verified, atomically. It
// returns how long the first blocked key stays blocked, or 0 when the attempt
// may proceed. At most MaxFailures attempts per Window proceed, however many
// run concurrently; the next one blocks the key. Call Reset on success.
func (l *Limiter) Take(ctx context.Context, keys ...string) (time.Duration, error) {
	for _, k := range keys {
		fails, block, strikes := l.keys(k)
		secs, err := takeScript.Run(ctx, l.rdb, []string{fails, block, strikes},
			l.cfg.MaxFailures,
			int64(l.cfg.Window/time.Second),
			int64(l.cfg.MaxBlock/time.Second),
			int64(strikesTTL/time.Second),
		).Int64()
		if err != nil {
			return 0, fmt.Errorf("limiter take: %w", err)
		}
		if secs > 0 {
			return time.Duration(secs) * time.Second, nil
		}
	}
	return 0, nil
}

// Reset clears attempts and strikes; an active block is left to expire.
func (l *Limiter) Reset(ctx context.Context, keys ...string) error {
	for _, k := range keys {
		fails, _, strikes := l.keys(k)
		if err := l.rdb.Del(ctx, fails, strikes).Err(); err != nil {
			return fmt.Errorf("limiter reset: %w", err)
		}
	}
	return nil
}
