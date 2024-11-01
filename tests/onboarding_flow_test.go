package tests

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/rajatgoel/ratchet/internal"
)

func TestOnboardingFlow(t *testing.T) {
	db := SetupStorage(t)

	ctx := context.Background()
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
