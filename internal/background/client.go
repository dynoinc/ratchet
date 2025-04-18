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
	var err error
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.AddBreadcrumb(&sentry.Breadcrumb{
			Category: "job",
			Message:  job.Kind,
			Level:    sentry.LevelInfo,
		}, 100)

		defer sentry.RecoverWithContext(ctx)

		if innerErr := doInner(ctx); innerErr != nil {
			sentry.CaptureException(innerErr)
			err = innerErr
		}
	})

	return err
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
