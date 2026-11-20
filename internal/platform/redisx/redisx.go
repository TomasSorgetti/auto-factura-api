// Package redisx opens the process Redis client.
package redisx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
)

// New opens a client with a small pool and bounded timeouts and verifies it
// with a ping. Errors never include the URL.
func New(ctx context.Context, url logx.Secret) (*redis.Client, error) {
	opts, err := options(url.Reveal())
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opts)

	pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := client.Ping(pctx).Err(); err != nil {
		if cerr := client.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return client, nil
}

func options(url string) (*redis.Options, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, errors.New("parse redis url")
	}
	opts.PoolSize = 5
	opts.MinIdleConns = 0
	opts.DialTimeout = 2 * time.Second
	opts.ReadTimeout = time.Second
	opts.WriteTimeout = time.Second
	opts.PoolTimeout = 2 * time.Second
	opts.ConnMaxIdleTime = 5 * time.Minute
	opts.ContextTimeoutEnabled = true
	return opts, nil
}
