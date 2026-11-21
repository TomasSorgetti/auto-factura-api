package main

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/TomasSorgetti/auto-factura-api/internal/clients"
	"github.com/TomasSorgetti/auto-factura-api/internal/platform/config"
)

func newClientsHandler(cfg config.Config, pool *pgxpool.Pool) (*clients.Handler, error) {
	codec, err := clients.NewCodec([]byte(cfg.MasterKey.Reveal()))
	if err != nil {
		return nil, fmt.Errorf("new clients codec: %w", err)
	}
	return clients.NewHandler(clients.NewService(pool, codec)), nil
}
