package main

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/TomasSorgetti/auto-factura-api/internal/auth"
	"github.com/TomasSorgetti/auto-factura-api/internal/emails"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/config"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/httpx"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/redisx"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/secrets"
)

func newAuth(cfg config.Config, log *slog.Logger, errs *httpx.ErrorMapper, pool *pgxpool.Pool, rdb *redis.Client) (*auth.Handler, httpx.Middleware, error) {
	cipher, err := secrets.NewCipher([]byte(cfg.MasterKey.Reveal()))
	if err != nil {
		return nil, nil, fmt.Errorf("new cipher: %w", err)
	}
	hasher, err := secrets.NewPasswordHasher(cfg.Argon2)
	if err != nil {
		return nil, nil, fmt.Errorf("new password hasher: %w", err)
	}
	store := auth.NewStore(pool)
	tokens, err := auth.NewTokens(auth.TokensConfig{
		AccessSecret: cfg.JWTAccessSecret, RefreshSecret: cfg.JWTRefreshSecret,
		AccessTTL: cfg.AccessTokenTTL, RefreshTTL: cfg.RefreshTokenTTL,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("new tokens: %w", err)
	}
	sessions := auth.NewSessions(store, tokens, log)
	limiter := redisx.NewLimiter(rdb, redisx.LimiterConfig{Prefix: "rl", MaxFailures: 5, Window: 15 * time.Minute, MaxBlock: 24 * time.Hour})
	passwordLimiter := redisx.NewLimiter(rdb, redisx.LimiterConfig{Prefix: "rl-pwd", MaxFailures: 50, Window: time.Hour, MaxBlock: 5 * time.Minute})
	refreshLimiter := redisx.NewLimiter(rdb, redisx.LimiterConfig{Prefix: "rl-refresh", MaxFailures: 30, Window: time.Minute, MaxBlock: 15 * time.Minute})

	var sender emails.Sender = emails.LogSender{Log: log}
	if cfg.ResendAPIKey != "" {
		sender = emails.NewResendSender(cfg.ResendAPIKey, cfg.EmailFrom)
	}
	devices, err := auth.NewDeviceTracker(auth.DeviceTrackerDeps{
		Store: store, Sender: sender, Log: log, Loc: cfg.BusinessLocation, BaseURL: cfg.PublicBaseURL,
	})
	if err != nil {
		return nil, nil, err
	}
	login, err := auth.NewLogin(auth.LoginDeps{
		Store: store, Sessions: sessions, Cipher: cipher, Hasher: hasher, Limiter: limiter, PasswordLimiter: passwordLimiter,
		Redis: rdb, Devices: devices, Log: log,
	})
	if err != nil {
		return nil, nil, err
	}
	h, err := auth.NewHandler(auth.HandlerDeps{
		Login: login, Sessions: sessions, Store: store, RefreshLimiter: refreshLimiter,
		AccessTTL: cfg.AccessTokenTTL, RefreshTTL: cfg.RefreshTokenTTL, Log: log,
	})
	if err != nil {
		return nil, nil, err
	}
	return h, auth.RequireAdmin(sessions, errs), nil
}
