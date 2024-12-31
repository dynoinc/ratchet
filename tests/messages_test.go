package tests

import (
	"context"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/stretchr/testify/require"

	"github.com/dynoinc/ratchet/internal"
)

func TestMessages(t *testing.T) {
	bot := SetupBot(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Run("can add messages to known channel", func(t *testing.T) {
		_, err := bot.AddChannel(ctx, "channel1")
		require.NoError(t, err)

		err = bot.AddMessages(
			ctx,
			"channel1",
			[]slack.Message{{
				Msg: slack.Msg{
					Timestamp: "1",
				},
			}},
			"watermark1",
		)
		require.NoError(t, err)
	})

	t.Run("fails to add message to unknown channel", func(t *testing.T) {
		err := bot.AddMessages(
			ctx,
			"channel2",
			[]slack.Message{{
				Msg: slack.Msg{
					Timestamp: "1",
				},
			}},
			"watermark1",
		)
		require.Error(t, err)
		require.ErrorIs(t, err, internal.ErrChannelNotKnown)
	})
}
