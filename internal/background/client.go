package background

import (
	"context"

	"github.com/getsentry/sentry-go"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
)

type sentryMiddleware struct {
	river.MiddlewareDefaults
}

func (m *sentryMiddleware) Work(ctx context.Context, job *rivertype.JobRow, doInner func(ctx context.Context) error) error {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub().Clone()
	}

	tx := sentry.StartTransaction(ctx, "worker.job", sentry.WithTransactionName(job.Kind))
	defer tx.Finish()

	ctx = sentry.SetHubOnContext(tx.Context(), hub)
	defer sentry.RecoverWithContext(ctx)

	if err := doInner(ctx); err != nil {
		sentry.CaptureException(err)
		tx.Status = sentry.SpanStatusInternalError
		return err
	} else {
		tx.Status = sentry.SpanStatusOK
	}

	return nil
}

func New(db *pgxpool.Pool, workers *river.Workers, periodicJobs []*river.PeriodicJob) (*river.Client[pgx.Tx], error) {
	return river.NewClient(riverpgxv5.New(db), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {
				MaxWorkers: 10,
			},
		},
		PeriodicJobs: periodicJobs,
		Workers:      workers,
		Middleware: []rivertype.Middleware{
			&sentryMiddleware{},
		},
	})
}
