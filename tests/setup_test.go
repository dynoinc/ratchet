package tests

import (
	"context"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage"
)

func SetupBot(t *testing.T) *internal.Bot {
	t.Helper()

	ctx := context.Background()
	postgresContainer, err := postgres.Run(ctx, storage.PostgresImage, postgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() { _ = postgresContainer.Stop(ctx, nil) })

	db, err := storage.New(ctx, postgresContainer.MustConnectionString(ctx, "sslmode=disable"))
	require.NoError(t, err)

	bot := internal.New(db)

	workers := river.NewWorkers()
	river.AddWorker(workers, river.WorkFunc(func(ctx context.Context, j *river.Job[background.ClassifierArgs]) error {
		return nil
	}))
	river.AddWorker(workers, river.WorkFunc(func(ctx context.Context, j *river.Job[background.MessagesIngestionWorkerArgs]) error {
		return nil
	}))

	riverClient, err := river.NewClient(riverpgxv5.New(db), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {
				MaxWorkers: 1,
			},
		},
		Workers: workers,
	})
	require.NoError(t, err)

	require.NoError(t, bot.Init(ctx, riverClient))
	return bot
}

func TestSetupBot(t *testing.T) {
	_ = SetupBot(t)
}
