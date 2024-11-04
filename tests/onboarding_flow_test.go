package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dynoinc/ratchet/internal"
)

func TestOnboardingFlow(t *testing.T) {
	db := SetupStorage(t)

	ctx := context.Background()
	bot, err := internal.New(db)
	require.NoError(t, err)

	t.Run("can add channel", func(t *testing.T) {
		err := bot.InsertOrEnableChannel(ctx, "channel1")
		require.NoError(t, err)
	})

	t.Run("inserting again works", func(t *testing.T) {
		err := bot.InsertOrEnableChannel(ctx, "channel1")
		require.NoError(t, err)
	})

	t.Run("can add multiple channels", func(t *testing.T) {
		err := bot.InsertOrEnableChannel(ctx, "channel2")
		require.NoError(t, err)
		err = bot.InsertOrEnableChannel(ctx, "channel3")
		require.NoError(t, err)
	})

	t.Run("can disable channel", func(t *testing.T) {
		err := bot.DisableChannel(ctx, "channel1")
		require.NoError(t, err)
	})
}
