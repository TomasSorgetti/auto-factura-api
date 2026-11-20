package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/pressly/goose/v3"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/config"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres"
)

func main() {
	if err := config.LoadDotEnv(".env", os.Getenv, os.Setenv); err != nil {
		fmt.Fprintf(os.Stderr, "load .env: %v\n", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Getenv, os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) (err error) {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	allowDown := fs.Bool("allow-down", false, "allow reverting a migration outside production")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("usage: migrate [--allow-down] up|down|status: %w", err)
	}
	if fs.NArg() == 0 {
		return errors.New("usage: migrate [--allow-down] up|down|status")
	}
	cmd := fs.Arg(0)
	if err := fs.Parse(fs.Args()[1:]); err != nil || fs.NArg() != 0 {
		return errors.New("usage: migrate [--allow-down] up|down|status")
	}
	switch cmd {
	case "up", "down", "status":
	default:
		return fmt.Errorf("usage: unknown command %q, want up, down or status", cmd)
	}

	cfg, err := config.LoadMigrate(getenv)
	if err != nil {
		return err
	}
	if cmd == "down" && (!*allowDown || cfg.Env == config.EnvProduction) {
		return errors.New("down is disabled: requires --allow-down and APP_ENV other than production")
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	log := logx.New(stdout, slog.LevelInfo, "text")

	pool, err := postgres.New(ctx, postgres.Config{
		URL:              cfg.DatabaseURL,
		MaxConns:         2,
		StatementTimeout: 5 * time.Minute,
		AppName:          "autofactura-migrate",
	})
	if err != nil {
		return err
	}
	defer pool.Close()

	m, err := postgres.NewMigrator(pool, true)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, m.Close()) }()

	switch cmd {
	case "up":
		results, err := m.Up(ctx)
		if err != nil {
			return fmt.Errorf("migrate up: %w", err)
		}
		if len(results) == 0 {
			log.Info("no migrations to apply")
		}
		for _, r := range results {
			log.Info("migration applied", "version", r.Source.Version, "source", r.Source.Path, "duration", r.Duration)
		}
	case "down":
		r, err := m.Down(ctx)
		if err != nil {
			return fmt.Errorf("migrate down: %w", err)
		}
		log.Info("migration reverted", "version", r.Source.Version, "source", r.Source.Path, "duration", r.Duration)
	case "status":
		statuses, err := m.Status(ctx)
		if err != nil {
			return fmt.Errorf("migrate status: %w", err)
		}
		for _, s := range statuses {
			if s.State == goose.StateApplied {
				log.Info("migration", "version", s.Source.Version, "applied_at", s.AppliedAt)
				continue
			}
			log.Info("migration", "version", s.Source.Version, "applied_at", "pending")
		}
	}
	return nil
}
