package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

func TestOnboardingFlow(t *testing.T) {
	bot := SetupBot(t)
	ctx := context.Background()

	t.Run("can add channel", func(t *testing.T) {
		_, err := bot.AddChannel(ctx, "channel1")
		require.NoError(t, err)
	})

	t.Run("inserting again works", func(t *testing.T) {
		_, err := bot.AddChannel(ctx, "channel1")
		require.NoError(t, err)
	})

	t.Run("can add multiple channels", func(t *testing.T) {
		_, err := bot.AddChannel(ctx, "channel2")
		require.NoError(t, err)
		_, err = bot.AddChannel(ctx, "channel3")
		require.NoError(t, err)
	})

	t.Run("listing channels works", func(t *testing.T) {
		channels, err := schema.New(bot.DB).GetChannels(ctx)
		require.NoError(t, err)
		// GetChannels should return all channels, even if they don't have names yet
		require.Len(t, channels, 3)
	})

	t.Run("can get channel without name", func(t *testing.T) {
		channel, err := schema.New(bot.DB).GetChannel(ctx, "channel1")
		require.NoError(t, err)
		require.Equal(t, "channel1", channel.ChannelID)
		require.Empty(t, channel.Attrs) // No attrs yet
	})

	t.Run("can get channel by name after attrs are set", func(t *testing.T) {
		queries := schema.New(bot.DB)

		err := queries.UpdateChannelAttrs(ctx, schema.UpdateChannelAttrsParams{
			ChannelID: "channel1",
			Attrs:     dto.ChannelAttrs{Name: "test-channel"},
		})
		require.NoError(t, err)

		// Now we should be able to find it by name
		channel, err := queries.GetChannelByName(ctx, "test-channel")
		require.NoError(t, err)
		require.Equal(t, "channel1", channel.ChannelID)
		require.Equal(t, "test-channel", channel.Attrs.Name)
	})
}
