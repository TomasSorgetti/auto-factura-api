// Package config reads and validates the process configuration.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
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

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
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
