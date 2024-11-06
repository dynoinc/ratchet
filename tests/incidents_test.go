package tests

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

func TestIncidents(t *testing.T) {
	bot := SetupBot(t)

	ctx := context.Background()
	t.Run("can open incident", func(t *testing.T) {
		err := bot.AddChannel(ctx, "channel1")
		require.NoError(t, err)

		err = bot.AddMessage(ctx, "channel1", "ts1", dto.MessageAttrs{})
		require.NoError(t, err)

		_, err = bot.OpenIncident(ctx, schema.OpenIncidentParams{
			ChannelID: "channel1",
			SlackTs:   "ts1",
			Alert:     "alert1",
			Service:   "service1",
			Priority:  "LOW",
			StartTimestamp: pgtype.Timestamptz{
				Time:  time.Now(),
				Valid: true,
			},
		})
		require.NoError(t, err)
	})

	t.Run("can close incident", func(t *testing.T) {
		err := bot.CloseIncident(ctx, "alert1", "service1", time.Now())
		require.NoError(t, err)
	})
}
