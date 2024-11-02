package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

func TestConversations(t *testing.T) {
	db := SetupStorage(t)

	ctx := context.Background()
	bot, err := internal.New(db)
	require.NoError(t, err)

	t.Run("attempting to start conversation to a channel not known fails", func(t *testing.T) {
		started, err := bot.StartConversation(ctx, "channel1", "user1", dto.MessageAttrs{})
		require.NoError(t, err)
		require.False(t, started)
	})

	t.Run("for onboarded channels it works", func(t *testing.T) {
		_, err := bot.InsertIntent(ctx, "channel1")
		require.NoError(t, err)

		err = bot.OnboardChannel(ctx, "channel1", "team1")
		require.NoError(t, err)

		started, err := bot.StartConversation(ctx, "channel1", "conv1", dto.MessageAttrs{})
		require.NoError(t, err)
		require.True(t, started)
	})

	t.Run("adding more messages to the conversation works", func(t *testing.T) {
		err := bot.AddMessage(ctx, "channel1", "conv1", "message1", dto.MessageAttrs{})
		require.NoError(t, err)
	})

	t.Run("adding message to non-existing conversation fails", func(t *testing.T) {
		err := bot.AddMessage(ctx, "channel1", "conv2", "message1", dto.MessageAttrs{})
		require.NoError(t, err)
	})
}
