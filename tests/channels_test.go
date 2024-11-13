package tests

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dynoinc/ratchet/internal/storage/schema"
)

func TestOnboardingFlow(t *testing.T) {
	bot := SetupBot(t)

	ctx := context.Background()
	t.Run("can add channel", func(t *testing.T) {
		err := bot.AddChannel(ctx, "channel1")
		require.NoError(t, err)
	})

	t.Run("inserting again works", func(t *testing.T) {
		err := bot.AddChannel(ctx, "channel1")
		require.NoError(t, err)
	})

	t.Run("can add multiple channels", func(t *testing.T) {
		err := bot.AddChannel(ctx, "channel2")
		require.NoError(t, err)
		err = bot.AddChannel(ctx, "channel3")
		require.NoError(t, err)
	})

	t.Run("listing channels works", func(t *testing.T) {
		channels, err := schema.New(bot.DB).GetChannels(ctx)
		require.NoError(t, err)
		require.Len(t, channels, 3)
		for _, id := range []string{"channel1", "channel2", "channel3"} {
			require.True(t, slices.ContainsFunc(channels, func(c schema.Channel) bool {
				return c.ChannelID == id
			}))
		}
	})
}
