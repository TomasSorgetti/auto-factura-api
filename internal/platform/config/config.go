// Package config reads and validates the process configuration.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/secrets"
)

type Env string

const (
	EnvDevelopment Env = "development"
	EnvTest        Env = "test"
	EnvProduction  Env = "production"
)

type Config struct {
	Env             Env
	HTTPAddr        string
	LogLevel        slog.Level
	LogFormat       string
	RequestTimeout  time.Duration
	ShutdownTimeout time.Duration

	CORSAllowedOrigins []string

	DatabaseURL        logx.Secret
	DBMaxConns         int32
	DBStatementTimeout time.Duration

	RedisURL logx.Secret

	MasterKey        logx.Secret
	JWTAccessSecret  logx.Secret
	JWTRefreshSecret logx.Secret
	AccessTokenTTL   time.Duration
	RefreshTokenTTL  time.Duration
	Argon2           secrets.Argon2Params
	ResendAPIKey     logx.Secret
	EmailFrom        string
	PublicBaseURL    string
	BusinessLocation *time.Location
}

func (c Config) IsProduction() bool { return c.Env == EnvProduction }

// MigrateConfig is the subset of the configuration that cmd/migrate needs.
type MigrateConfig struct {
	Env         Env
	DatabaseURL logx.Secret
}

// LoadMigrate reads only APP_ENV and DATABASE_URL through getenv.
func LoadMigrate(getenv func(string) string) (MigrateConfig, error) {
	var errs []error
	env, err := parseEnv(getenv)
	if err != nil {
		errs = append(errs, err)
	}
	dbURL, err := parseDatabaseURL(getenv)
	if err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return MigrateConfig{}, errors.Join(errs...)
	}
	return MigrateConfig{Env: env, DatabaseURL: dbURL}, nil
}

// AdminConfig is the subset of the configuration that cmd/admin needs.
type AdminConfig struct {
	Env         Env
	DatabaseURL logx.Secret
	Argon2      secrets.Argon2Params
}

// LoadAdmin reads APP_ENV, DATABASE_URL and ARGON2_* through getenv.
func LoadAdmin(getenv func(string) string) (AdminConfig, error) {
	var errs []error
	env, err := parseEnv(getenv)
	if err != nil {
		errs = append(errs, err)
	}
	dbURL, err := parseDatabaseURL(getenv)
	if err != nil {
		errs = append(errs, err)
	}
	argon2, argonErrs := loadArgon2(getenv)
	errs = append(errs, argonErrs...)
	if len(errs) > 0 {
		return AdminConfig{}, errors.Join(errs...)
	}
	return AdminConfig{Env: env, DatabaseURL: dbURL, Argon2: argon2}, nil
}

func parseEnv(getenv func(string) string) (Env, error) {
	switch env := Env(getenv("APP_ENV")); env {
	case EnvDevelopment, EnvTest, EnvProduction:
		return env, nil
	default:
		return "", errors.New("APP_ENV: must be one of development, test, production")
	}
}

func parseDatabaseURL(getenv func(string) string) (logx.Secret, error) {
	v := getenv("DATABASE_URL")
	if v == "" {
		return "", errors.New("DATABASE_URL: is required")
	}
	if _, err := pgxpool.ParseConfig(v); err != nil {
		return "", errors.New("DATABASE_URL: invalid connection string")
	}
	return logx.Secret(v), nil
}

func parseRedisURL(getenv func(string) string) (logx.Secret, error) {
	v := getenv("REDIS_URL")
	if v == "" {
		return "", errors.New("REDIS_URL: is required")
	}
	if _, err := redis.ParseURL(v); err != nil {
		return "", errors.New("REDIS_URL: invalid url")
	}
	return logx.Secret(v), nil
}

// Load reads the configuration through getenv and reports every invalid
// variable at once.
func Load(getenv func(string) string) (Config, error) {
	var errs []error
	cfg := Config{
		HTTPAddr:        ":8080",
		LogLevel:        slog.LevelInfo,
		LogFormat:       "json",
		RequestTimeout:  15 * time.Second,
		ShutdownTimeout: 10 * time.Second,
	}

	cfg.DBMaxConns = 5
	cfg.DBStatementTimeout = 5 * time.Second

	if env, err := parseEnv(getenv); err != nil {
		errs = append(errs, err)
	} else {
		cfg.Env = env
	}

	if dbURL, err := parseDatabaseURL(getenv); err != nil {
		errs = append(errs, err)
	} else {
		cfg.DatabaseURL = dbURL
	}

	if redisURL, err := parseRedisURL(getenv); err != nil {
		errs = append(errs, err)
	} else {
		cfg.RedisURL = redisURL
	}

	if v := getenv("DB_MAX_CONNS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 20 {
			errs = append(errs, errors.New("DB_MAX_CONNS: must be an integer between 1 and 20"))
		} else {
			cfg.DBMaxConns = int32(n)
		}
	}

	if d, err := durationBetween(getenv, "DB_STATEMENT_TIMEOUT", cfg.DBStatementTimeout, time.Second, time.Minute); err != nil {
		errs = append(errs, err)
	} else {
		cfg.DBStatementTimeout = d
	}

	if v := getenv("HTTP_ADDR"); v != "" {
		cfg.HTTPAddr = v
	}

	if v := getenv("LOG_LEVEL"); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(v)); err != nil {
			errs = append(errs, errors.New("LOG_LEVEL: must be one of debug, info, warn, error"))
		}
	}

	if v := getenv("LOG_FORMAT"); v != "" {
		switch v {
		case "json", "text":
			cfg.LogFormat = v
		default:
			errs = append(errs, errors.New("LOG_FORMAT: must be one of json, text"))
		}
	}
	if cfg.IsProduction() && cfg.LogFormat != "json" {
		errs = append(errs, errors.New("LOG_FORMAT: must be json in production"))
	}

	if d, err := duration(getenv, "REQUEST_TIMEOUT", cfg.RequestTimeout, time.Minute); err != nil {
		errs = append(errs, err)
	} else {
		cfg.RequestTimeout = d
	}
	if d, err := duration(getenv, "SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout, 30*time.Second); err != nil {
		errs = append(errs, err)
	} else {
		cfg.ShutdownTimeout = d
	}

	if v := getenv("CORS_ALLOWED_ORIGINS"); v != "" {
		origins, err := parseOrigins(v, cfg.IsProduction())
		if err != nil {
			errs = append(errs, err)
		} else {
			cfg.CORSAllowedOrigins = origins
		}
	}

	errs = append(errs, loadAuth(getenv, &cfg)...)

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

func loadAuth(getenv func(string) string, cfg *Config) []error {
	var errs []error

	if key, err := parseMasterKey(getenv("MASTER_KEY")); err != nil {
		errs = append(errs, err)
	} else {
		cfg.MasterKey = key
	}

	access := getenv("JWT_ACCESS_SECRET")
	refresh := getenv("JWT_REFRESH_SECRET")
	if len(access) < minJWTSecretLen {
		errs = append(errs, errors.New("JWT_ACCESS_SECRET: is required and must be at least 32 bytes"))
	}
	if len(refresh) < minJWTSecretLen {
		errs = append(errs, errors.New("JWT_REFRESH_SECRET: is required and must be at least 32 bytes"))
	}
	if access != "" && access == refresh {
		errs = append(errs, errors.New("JWT_REFRESH_SECRET: must differ from JWT_ACCESS_SECRET"))
	}
	cfg.JWTAccessSecret = logx.Secret(access)
	cfg.JWTRefreshSecret = logx.Secret(refresh)

	var err error
	if cfg.AccessTokenTTL, err = durationBetween(getenv, "ACCESS_TOKEN_TTL", 15*time.Minute, time.Minute, time.Hour); err != nil {
		errs = append(errs, err)
	}
	if cfg.RefreshTokenTTL, err = durationBetween(getenv, "REFRESH_TOKEN_TTL", 168*time.Hour, time.Hour, 720*time.Hour); err != nil {
		errs = append(errs, err)
	}

	argon2, argonErrs := loadArgon2(getenv)
	cfg.Argon2 = argon2
	errs = append(errs, argonErrs...)

	cfg.ResendAPIKey = logx.Secret(getenv("RESEND_API_KEY"))
	if cfg.IsProduction() && cfg.ResendAPIKey == "" {
		errs = append(errs, errors.New("RESEND_API_KEY: is required in production"))
	}
	if from := getenv("EMAIL_FROM"); from != "" {
		if _, err := mail.ParseAddress(from); err != nil {
			errs = append(errs, errors.New("EMAIL_FROM: must be a valid address such as Name <user@example.com>"))
		} else {
			cfg.EmailFrom = from
		}
	} else if cfg.ResendAPIKey != "" {
		errs = append(errs, errors.New("EMAIL_FROM: is required when RESEND_API_KEY is set"))
	}

	cfg.PublicBaseURL = "http://localhost:5173"
	if v := getenv("PUBLIC_BASE_URL"); v != "" {
		cfg.PublicBaseURL = v
	}
	if err := validateBaseURL(cfg.PublicBaseURL, cfg.IsProduction()); err != nil {
		errs = append(errs, err)
	}

	tz := "America/Argentina/Buenos_Aires"
	if v := getenv("BUSINESS_TZ"); v != "" {
		tz = v
	}
	if loc, err := time.LoadLocation(tz); err != nil {
		errs = append(errs, errors.New("BUSINESS_TZ: must be a valid IANA time zone"))
	} else {
		cfg.BusinessLocation = loc
	}

	return errs
}

func loadArgon2(getenv func(string) string) (secrets.Argon2Params, []error) {
	var errs []error
	p := secrets.Argon2Params{MemoryKiB: 19456, Time: 2, Threads: 1}
	if v, err := uintBetween(getenv, "ARGON2_MEMORY_KIB", 8192, 262144); err != nil {
		errs = append(errs, err)
	} else if v != 0 {
		p.MemoryKiB = uint32(v)
	}
	if v, err := uintBetween(getenv, "ARGON2_TIME", 1, 10); err != nil {
		errs = append(errs, err)
	} else if v != 0 {
		p.Time = uint32(v)
	}
	if v, err := uintBetween(getenv, "ARGON2_THREADS", 1, 8); err != nil {
		errs = append(errs, err)
	} else if v != 0 {
		p.Threads = uint8(v)
	}
	return p, errs
}

const minJWTSecretLen = 32

func parseMasterKey(v string) (logx.Secret, error) {
	if v == "" {
		return "", errors.New("MASTER_KEY: is required")
	}
	b, err := base64.StdEncoding.DecodeString(v)
	if err != nil || len(b) != 32 {
		return "", errors.New("MASTER_KEY: must be base64 of exactly 32 bytes")
	}
	return logx.Secret(string(b)), nil
}

func validateBaseURL(v string, production bool) error {
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return errors.New("PUBLIC_BASE_URL: must be http(s)://host[:port]")
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil || u.Opaque != "" {
		return errors.New("PUBLIC_BASE_URL: must not have a path, query or user info")
	}
	if production && u.Scheme != "https" {
		return errors.New("PUBLIC_BASE_URL: must use https in production")
	}
	return nil
}

// uintBetween returns 0 when the variable is unset.
func uintBetween(getenv func(string) string, key string, lo, hi uint64) (uint64, error) {
	v := getenv(key)
	if v == "" {
		return 0, nil
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%s: must be an integer between %d and %d", key, lo, hi)
	}
	return n, nil
}

func parseOrigins(list string, production bool) ([]string, error) {
	parts := strings.Split(list, ",")
	origins := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		u, err := url.Parse(p)
		if err != nil || p == "*" {
			return nil, fmt.Errorf("CORS_ALLOWED_ORIGINS: %q is not a valid origin", p)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("CORS_ALLOWED_ORIGINS: %q must use http or https", p)
		}
		if u.Host == "" || u.Hostname() == "" {
			return nil, fmt.Errorf("CORS_ALLOWED_ORIGINS: %q has no host", p)
		}
		if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil || u.Opaque != "" {
			return nil, fmt.Errorf("CORS_ALLOWED_ORIGINS: %q must be scheme://host[:port] only", p)
		}
		if production && u.Scheme != "https" {
			return nil, fmt.Errorf("CORS_ALLOWED_ORIGINS: %q must use https in production", p)
		}
		host := strings.ToLower(u.Hostname())
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		port := u.Port()
		if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
			port = ""
		}
		if port != "" {
			host += ":" + port
		}
		origins = append(origins, u.Scheme+"://"+host)
	}
	return origins, nil
}

func duration(getenv func(string) string, key string, def, limit time.Duration) (time.Duration, error) {
	return durationBetween(getenv, key, def, 1, limit)
}

func durationBetween(getenv func(string) string, key string, def, lo, hi time.Duration) (time.Duration, error) {
	v := getenv(key)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: must be a duration such as 15s", key)
	}
	if d < lo || d > hi {
		if lo == 1 {
			return 0, fmt.Errorf("%s: must be greater than 0 and at most %s", key, hi)
		}
		return 0, fmt.Errorf("%s: must be between %s and %s", key, lo, hi)
	}
	return d, nil
}
