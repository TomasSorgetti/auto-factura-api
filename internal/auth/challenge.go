package auth

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/secrets"
)

type challenge struct {
	userID   string
	issuedAt time.Time
	pending  []byte
}

// challengeStore keeps login challenges in Redis keyed by the hash of an opaque token.
type challengeStore struct {
	rdb    *redis.Client
	prefix string
	now    func() time.Time
}

func (c *challengeStore) key(token string) string {
	return c.prefix + ":" + hex.EncodeToString(secrets.HashToken(token))
}

func (c *challengeStore) create(ctx context.Context, userID string) (string, time.Time, error) {
	token, err := secrets.RandomToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	issuedAt := c.now()
	expiresAt := issuedAt.Add(challengeTTL)
	key := c.key(token)
	_, err = c.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.HSet(ctx, key, "uid", userID, "iat", issuedAt.UnixNano(), "attempts", 0)
		p.Expire(ctx, key, challengeTTL)
		return nil
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("create challenge: %w", err)
	}
	return token, expiresAt, nil
}

func (c *challengeStore) get(ctx context.Context, token string) (challenge, error) {
	m, err := c.rdb.HGetAll(ctx, c.key(token)).Result()
	if err != nil {
		return challenge{}, fmt.Errorf("get challenge: %w", err)
	}
	uid := m["uid"]
	if uid == "" {
		return challenge{}, ErrChallengeInvalid
	}
	nanos, err := strconv.ParseInt(m["iat"], 10, 64)
	if err != nil {
		return challenge{}, ErrChallengeInvalid
	}
	ch := challenge{userID: uid, issuedAt: time.Unix(0, nanos)}
	if p := m["pending"]; p != "" {
		raw, err := base64.StdEncoding.DecodeString(p)
		if err != nil {
			return challenge{}, ErrChallengeInvalid
		}
		ch.pending = raw
	}
	return ch, nil
}

var (
	setPendingScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
redis.call('HSET', KEYS[1], 'pending', ARGV[1])
return 1
`)
	attemptScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
local n = redis.call('HINCRBY', KEYS[1], 'attempts', 1)
if n > tonumber(ARGV[1]) then
	redis.call('DEL', KEYS[1])
	return 0
end
return n
`)
)

func (c *challengeStore) setPending(ctx context.Context, token string, sealed []byte) error {
	ok, err := setPendingScript.Run(ctx, c.rdb, []string{c.key(token)}, base64.StdEncoding.EncodeToString(sealed)).Int()
	if err != nil {
		return fmt.Errorf("set pending secret: %w", err)
	}
	if ok == 0 {
		return ErrChallengeInvalid
	}
	return nil
}

// attempt counts one verification try before it runs. Past challengeTries the
// challenge is deleted and ErrChallengeInvalid is returned.
func (c *challengeStore) attempt(ctx context.Context, token string) error {
	n, err := attemptScript.Run(ctx, c.rdb, []string{c.key(token)}, challengeTries).Int()
	if err != nil {
		return fmt.Errorf("challenge attempt: %w", err)
	}
	if n == 0 {
		return ErrChallengeInvalid
	}
	return nil
}

func (c *challengeStore) delete(ctx context.Context, token string) error {
	if err := c.rdb.Del(ctx, c.key(token)).Err(); err != nil {
		return fmt.Errorf("delete challenge: %w", err)
	}
	return nil
}
