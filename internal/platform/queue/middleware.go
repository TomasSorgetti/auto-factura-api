package queue

import (
	"context"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/TomasSorgetti/auto-factura-api/internal/platform/logx"
)

type jobLogging struct {
	river.MiddlewareDefaults
}

func (*jobLogging) Work(ctx context.Context, job *rivertype.JobRow, doInner func(context.Context) error) error {
	return doInner(logx.WithJob(ctx, job.ID, job.Kind))
}
