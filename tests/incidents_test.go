package tests

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"
	"github.com/stretchr/testify/require"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

func TestIncidents(t *testing.T) {
	ctx := context.Background()
	bot := SetupBot(t)

	now := time.Now().Round(time.Millisecond)
	stz := pgtype.Timestamptz{
		Time:  now.Add(-time.Hour),
		Valid: true,
	}
	etz := pgtype.Timestamptz{
		Time:  now,
		Valid: true,
	}

	t.Run("closing an incident that doesn't exist returns an error", func(t *testing.T) {
		err := bot.CloseIncident(ctx, "channel1", "ts1", "alert1", "service1", etz)
		require.Error(t, err)
		require.Equal(t, internal.ErrNoOpenIncident, err)
	})

	t.Run("can open incident", func(t *testing.T) {
		err := bot.AddChannel(ctx, "channel1")
		require.NoError(t, err)

		err = bot.AddMessages(ctx, "channel1", []slack.Message{
			{
				Msg: slack.Msg{
					Timestamp: "ts1",
				},
			},
		})
		require.NoError(t, err)

		_, err = bot.OpenIncident(ctx, schema.OpenIncidentParams{
			ChannelID:      "channel1",
			SlackTs:        "ts1",
			Alert:          "alert1",
			Service:        "service1",
			Priority:       "LOW",
			StartTimestamp: stz,
		})
		require.NoError(t, err)
	})

	t.Run("can close incident", func(t *testing.T) {
		err := bot.CloseIncident(ctx, "channel1", "ts1", "alert1", "service1", etz)
		require.NoError(t, err)
	})

	t.Run("closes the right incident if multiple incidents are open", func(t *testing.T) {
		err := bot.AddMessages(ctx, "channel1",
			[]slack.Message{
				{
					Msg: slack.Msg{
						Timestamp: "ts2",
					},
				},
			})
		require.NoError(t, err)

		stz1 := stz
		stz1.Time = stz.Time.Add(-time.Hour)
		_, err = bot.OpenIncident(ctx, schema.OpenIncidentParams{
			ChannelID:      "channel1",
			SlackTs:        "ts2",
			Alert:          "alert1",
			Service:        "service1",
			Priority:       "LOW",
			StartTimestamp: stz1,
		})
		require.NoError(t, err)

		err = bot.AddMessages(ctx, "channel1", []slack.Message{
			{
				Msg: slack.Msg{
					Timestamp: "ts3",
				},
			},
		})
		require.NoError(t, err)

		stz2 := stz
		stz2.Time = stz.Time.Add(-30 * time.Minute)
		_, err = bot.OpenIncident(ctx, schema.OpenIncidentParams{
			ChannelID:      "channel1",
			SlackTs:        "ts3",
			Alert:          "alert1",
			Service:        "service1",
			Priority:       "LOW",
			StartTimestamp: stz2,
		})
		require.NoError(t, err)

		err = bot.AddMessages(ctx, "channel1", []slack.Message{
			{
				Msg: slack.Msg{
					Timestamp: "ts4",
				},
			},
		})
		require.NoError(t, err)

		err = bot.CloseIncident(ctx, "channel1", "ts4", "alert1", "service1", etz)
		require.NoError(t, err)

		// Closes the incident that was opened immediately before the end timestamp.
		incidents, err := schema.New(bot.DB).GetOpenIncidents(ctx)
		require.NoError(t, err)
		require.Len(t, incidents, 1)
		require.Equal(t, stz1.Time, incidents[0].StartTimestamp.Time)
	})
}
