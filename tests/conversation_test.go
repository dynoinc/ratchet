package tests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

func TestConversations(t *testing.T) {
	db := SetupStorage(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	riverClient, err := background.New(db)
	require.NoError(t, err)
	bot, err := internal.New(db, riverClient)
	require.NoError(t, err)

	t.Run("can add messages to known channel", func(t *testing.T) {
		err := bot.AddChannel(ctx, "channel1")
		require.NoError(t, err)

		err = bot.AddMessage(ctx, "channel1", "conv1", dto.MessageAttrs{})
		require.NoError(t, err)
	})

	t.Run("fails to add message to unknown channel", func(t *testing.T) {
		err := bot.AddMessage(ctx, "channel2", "conv2", dto.MessageAttrs{})
		require.Error(t, err)
		require.ErrorIs(t, err, internal.ErrChannelNotKnown)
	})
}
