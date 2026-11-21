package settings

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TomasSorgetti/auto-factura-api/internal/audit"
	"github.com/TomasSorgetti/auto-factura-api/internal/cuit"
	"github.com/TomasSorgetti/auto-factura-api/internal/db/sqlc"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/postgres"
)

type Service struct {
	pool *pgxpool.Pool
	loc  *time.Location
	now  func() time.Time
}

func NewService(pool *pgxpool.Pool, loc *time.Location) *Service {
	return &Service{pool: pool, loc: loc, now: time.Now}
}

// Get returns ErrNotConfigured before the first Update.
func (s *Service) Get(ctx context.Context) (Settings, error) {
	return get(ctx, sqlc.New(s.pool), false)
}

// Update validates in, stores it and records settings.updated in the same transaction when something changed.
func (s *Service) Update(ctx context.Context, actor audit.Actor, in Input) (Settings, error) {
	fields, err := in.Validate(s.now().In(s.loc))
	if err != nil {
		return Settings{}, err
	}
	var out Settings
	err = postgres.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		current, err := get(ctx, q, true)
		previous := current.Fields
		if errors.Is(err, ErrNotConfigured) {
			previous = Fields{}
		} else if err != nil {
			return err
		}
		changed := DiffFields(previous, fields)
		if changed == "" && err == nil {
			out = current
			return nil
		}
		if out, err = upsert(ctx, q, fields); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Event{
			Actor: actor, Action: audit.ActionSettingsUpdated, EntityType: "settings",
			Data: map[string]string{"fields": changed},
		}); err != nil {
			return fmt.Errorf("audit settings update: %w", err)
		}
		return nil
	})
	if err != nil {
		return Settings{}, err
	}
	return out, nil
}

// SetArcaEnv switches the ARCA environment and returns the previous one; from == env means nothing was written.
// Moving to production requires confirmCUIT to match the stored CUIT.
func (s *Service) SetArcaEnv(ctx context.Context, env ArcaEnv, confirmCUIT string, actor audit.Actor) (ArcaEnv, error) {
	switch env {
	case ArcaHomologation, ArcaProduction:
	default:
		return "", ErrInvalidArcaEnv
	}
	var from ArcaEnv
	err := postgres.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := sqlc.New(tx)
		current, err := get(ctx, q, true)
		if err != nil {
			return err
		}
		from = current.ArcaEnv
		if from == env {
			return nil
		}
		if env == ArcaProduction && cuit.Normalize(confirmCUIT) != current.CUIT {
			return ErrConfirmationMismatch
		}
		if _, err := q.SetArcaEnv(ctx, string(env)); err != nil {
			return fmt.Errorf("set arca env: %w", err)
		}
		if err := audit.Record(ctx, tx, audit.Event{
			Actor: actor, Action: audit.ActionArcaEnvChanged, EntityType: "settings",
			Data: map[string]string{"from": string(from), "to": string(env)},
		}); err != nil {
			return fmt.Errorf("audit arca env change: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return from, nil
}
