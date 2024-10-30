package tests

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/rajatgoel/ratchet/internal"
	"github.com/rajatgoel/ratchet/internal/storage"
)

func TestOnboardingFlow(t *testing.T) {
	ctx := context.Background()
	postgresContainer, err := postgres.Run(ctx, "postgres:latest", postgres.BasicWaitStrategies())
	require.NoError(t, err)

	db, err := storage.NewDBConnectionWithURL(ctx, postgresContainer.MustConnectionString(ctx, "sslmode=disable"))
	require.NoError(t, err)

	bot, err := internal.New(db)
	require.NoError(t, err)

	t.Run("intent doesn't exist", func(t *testing.T) {
		err := bot.OnboardChannel(ctx, "channel1", "team1")
		require.Error(t, err)
		require.Equal(t, pgx.ErrNoRows, err)
	})

	t.Run("onboard channel", func(t *testing.T) {
		_, err := bot.InsertIntent(ctx, "channel1")
		require.NoError(t, err)

		err = bot.OnboardChannel(ctx, "channel1", "team1")
		require.NoError(t, err)
	})

	t.Run("onboarded channel again fails", func(t *testing.T) {
		err := bot.OnboardChannel(ctx, "channel1", "team1")
		require.Error(t, err)
		require.Contains(t, err.Error(), "channel channel1 is already onboarded")
	})
}
