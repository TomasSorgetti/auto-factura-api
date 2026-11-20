// Command admin creates the single admin user and runs rescue operations from a console.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/TomasSorgetti/auto-factura-api/internal/auth"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/config"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/secrets"
)

var errWriteFailed = errors.New("could not write the generated password; the account was changed: run reset-password to set a new one")

const (
	usage            = "usage: admin create-user --email <email> [--password-stdin] | reset-password [--password-stdin] | reset-totp | revoke-sessions | prune-tokens"
	maxPasswordInput = 256
)

func main() {
	if err := config.LoadDotEnv(".env", os.Getenv, os.Setenv); err != nil {
		fmt.Fprintf(os.Stderr, "load .env: %v\n", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:], os.Getenv, os.Stdin, os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "admin: %v\n", err)
		os.Exit(1)
	}
}

type options struct {
	cmd           string
	email         string
	passwordStdin bool
}

func parseArgs(args []string) (options, error) {
	if len(args) == 0 {
		return options{}, errors.New(usage)
	}
	opts := options{cmd: args[0]}
	fs := flag.NewFlagSet("admin "+opts.cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	switch opts.cmd {
	case "create-user":
		fs.StringVar(&opts.email, "email", "", "admin email")
		fs.BoolVar(&opts.passwordStdin, "password-stdin", false, "read the password from stdin")
	case "reset-password":
		fs.BoolVar(&opts.passwordStdin, "password-stdin", false, "read the password from stdin")
	case "reset-totp", "revoke-sessions", "prune-tokens":
	default:
		return options{}, fmt.Errorf("unknown command %q: %s", opts.cmd, usage)
	}
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return options{}, errors.New(usage)
	}
	if opts.cmd == "create-user" && strings.TrimSpace(opts.email) == "" {
		return options{}, errors.New("--email is required: " + usage)
	}
	return opts, nil
}

func run(ctx context.Context, args []string, getenv func(string) string, stdin io.Reader, stdout io.Writer) error {
	opts, err := parseArgs(args)
	if err != nil {
		return err
	}
	cfg, err := config.LoadAdmin(getenv)
	if err != nil {
		return err
	}
	hasher, err := secrets.NewPasswordHasher(cfg.Argon2)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	pool, err := postgres.New(ctx, postgres.Config{
		URL:              cfg.DatabaseURL,
		MaxConns:         1,
		StatementTimeout: 30 * time.Second,
		AppName:          "autofactura-admin",
	})
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := postgres.CheckSchema(ctx, pool); err != nil {
		return err
	}

	accounts := auth.NewAccounts(pool, hasher)
	switch opts.cmd {
	case "create-user":
		password, generated, err := obtainPassword(stdin, opts.passwordStdin)
		if err != nil {
			return err
		}
		email := strings.ToLower(strings.TrimSpace(opts.email))
		if _, err := accounts.CreateUser(ctx, email, password); err != nil {
			return err
		}
		return report(stdout, "admin user created: "+email, password, generated)
	case "reset-password":
		password, generated, err := obtainPassword(stdin, opts.passwordStdin)
		if err != nil {
			return err
		}
		if err := accounts.ResetPassword(ctx, password); err != nil {
			return err
		}
		return report(stdout, "password reset; all sessions revoked", password, generated)
	case "reset-totp":
		if err := accounts.ResetTOTP(ctx); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "totp reset; recovery codes deleted; all sessions revoked; next login will ask to enroll again")
	case "revoke-sessions":
		if err := accounts.RevokeSessions(ctx); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "all sessions revoked")
	case "prune-tokens":
		n, err := accounts.PruneExpiredTokens(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(stdout, "pruned %d expired refresh tokens\n", n)
	}
	return nil
}

func report(stdout io.Writer, line, password string, generated bool) error {
	if _, err := fmt.Fprintln(stdout, line); err != nil {
		return errWriteFailed
	}
	if generated {
		if _, err := fmt.Fprintf(stdout, "generated password (shown once): %s\n", password); err != nil {
			return errWriteFailed
		}
	}
	return nil
}

func obtainPassword(stdin io.Reader, fromStdin bool) (password string, generated bool, err error) {
	if fromStdin {
		password, err = readPassword(stdin)
		return password, false, err
	}
	password, err = secrets.RandomToken(18)
	return password, true, err
}

func readPassword(stdin io.Reader) (string, error) {
	r := bufio.NewReader(io.LimitReader(stdin, maxPasswordInput+2))
	line, err := r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read password from stdin: %w", err)
	}
	line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	if line == "" {
		return "", errors.New("password from stdin is empty")
	}
	if len(line) > maxPasswordInput {
		return "", errors.New("password from stdin is too long")
	}
	return line, nil
}
