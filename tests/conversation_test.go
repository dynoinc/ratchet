package tests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

func TestConversations(t *testing.T) {
	db := SetupStorage(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bot, err := internal.New(db)
	require.NoError(t, err)

	t.Run("can add messages to channel without knowing about them", func(t *testing.T) {
		err := bot.AddMessage(ctx, "channel1", "conv1", dto.MessageAttrs{})
		require.NoError(t, err)
	})

	t.Run("can add messages to known channel", func(t *testing.T) {
		err := bot.InsertOrEnableChannel(ctx, "channel1")
		require.NoError(t, err)

		err = bot.AddMessage(ctx, "channel1", "conv1", dto.MessageAttrs{})
		require.NoError(t, err)
	})
}
