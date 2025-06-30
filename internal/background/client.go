package background

import (
	"context"

	"github.com/getsentry/sentry-go"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
	"github.com/riverqueue/rivercontrib/otelriver"
)

type sentryMiddleware struct {
	river.MiddlewareDefaults
}

func (m *sentryMiddleware) Work(ctx context.Context, job *rivertype.JobRow, doInner func(ctx context.Context) error) error {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub().Clone()
		ctx = sentry.SetHubOnContext(ctx, hub)
	}

	client := hub.Client()
	scope := hub.Scope()

	defer client.RecoverWithContext(ctx, nil, &sentry.EventHint{Context: ctx}, scope)

	if err := doInner(ctx); err != nil {
		client.CaptureException(err,
			&sentry.EventHint{Context: ctx},
			scope)
		return err
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
			otelriver.NewMiddleware(&otelriver.MiddlewareConfig{
				EnableWorkSpanJobKindSuffix: true,
			}),
			&sentryMiddleware{},
		},
	})
}
