package settings

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/TomasSorgetti/auto-factura-api/internal/db/sqlc"
	"github.com/TomasSorgetti/auto-factura-api/internal/money"
)

func fromRow(r sqlc.Setting) Settings {
	return Settings{
		Fields: Fields{
			CUIT:              r.Cuit,
			LegalName:         r.LegalName,
			Address:           r.Address,
			GrossIncomeNumber: r.GrossIncomeNumber,
			ActivityStartDate: r.ActivityStartDate.Time.UTC(),
			PointOfSale:       r.PointOfSale,
			Category:          Category(r.MonotributoCategory),
			AnnualCap:         money.Cents(r.AnnualCapCents),
		},
		ArcaEnv:   ArcaEnv(r.ArcaEnv),
		UpdatedAt: r.UpdatedAt.Time,
	}
}

func get(ctx context.Context, q *sqlc.Queries, forUpdate bool) (Settings, error) {
	var (
		row sqlc.Setting
		err error
	)
	if forUpdate {
		row, err = q.GetSettingsForUpdate(ctx)
	} else {
		row, err = q.GetSettings(ctx)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{}, ErrNotConfigured
	}
	if err != nil {
		return Settings{}, fmt.Errorf("get settings: %w", err)
	}
	return fromRow(row), nil
}

func upsert(ctx context.Context, q *sqlc.Queries, f Fields) (Settings, error) {
	row, err := q.UpsertSettings(ctx, sqlc.UpsertSettingsParams{
		Cuit:                f.CUIT,
		LegalName:           f.LegalName,
		Address:             f.Address,
		GrossIncomeNumber:   f.GrossIncomeNumber,
		ActivityStartDate:   pgtype.Date{Time: f.ActivityStartDate, Valid: true},
		PointOfSale:         f.PointOfSale,
		MonotributoCategory: string(f.Category),
		AnnualCapCents:      int64(f.AnnualCap),
	})
	if err != nil {
		return Settings{}, fmt.Errorf("upsert settings: %w", err)
	}
	return fromRow(row), nil
}
