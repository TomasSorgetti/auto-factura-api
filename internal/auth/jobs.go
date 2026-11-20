package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/queue"
)

type PruneTokensArgs struct{}

func (PruneTokensArgs) Kind() string { return "auth.prune_tokens" }

func (PruneTokensArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       queue.QueueDefault,
		MaxAttempts: 3,
		UniqueOpts:  river.UniqueOpts{ByPeriod: 24 * time.Hour},
	}
}

type PruneTokensWorker struct {
	river.WorkerDefaults[PruneTokensArgs]
	Accounts *Accounts
	Log      *slog.Logger
}

// Work deletes the expired refresh tokens and logs how many.
func (w *PruneTokensWorker) Work(ctx context.Context, _ *river.Job[PruneTokensArgs]) error {
	n, err := w.Accounts.PruneExpiredTokens(ctx)
	if errors.Is(err, ErrNotFound) {
		w.Log.InfoContext(ctx, "no admin user, nothing to prune")
		return nil
	}
	if err != nil {
		return fmt.Errorf("prune refresh tokens: %w", err)
	}
	w.Log.InfoContext(ctx, "pruned refresh tokens", "count", n)
	return nil
}

// PruneTokensPeriodic runs the prune every 24 hours and once on worker start.
func PruneTokensPeriodic() *river.PeriodicJob {
	return river.NewPeriodicJob(
		river.PeriodicInterval(24*time.Hour),
		func() (river.JobArgs, *river.InsertOpts) { return PruneTokensArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true},
	)
}
