package storage

import (
	"context"
	"testing"

	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestDBSetup(t *testing.T) {
	ctx := context.Background()
	postgresContainer, err := postgres.Run(ctx, postgresImage, postgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() { _ = postgresContainer.Stop(ctx, nil) })

	_, err = New(ctx, postgresContainer.MustConnectionString(ctx, "sslmode=disable"))
	require.NoError(t, err)
}

func TestUpdateReaction(t *testing.T) {
	ctx := context.Background()
	postgresContainer, err := postgres.Run(ctx, postgresImage, postgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() { _ = postgresContainer.Stop(ctx, nil) })

	db, err := New(ctx, postgresContainer.MustConnectionString(ctx, "sslmode=disable"))
	require.NoError(t, err)

	// add channel
	_, err = schema.New(db).AddChannel(ctx, "C0706000000")
	require.NoError(t, err)

	err = schema.New(db).AddMessage(ctx, schema.AddMessageParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
		Attrs:     dto.MessageAttrs{},
	})
	require.NoError(t, err)

	err = schema.New(db).UpdateReaction(ctx, schema.UpdateReactionParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
		Reaction:  "thumbsup",
		Count:     1,
	})
	require.NoError(t, err)

	msg, err := schema.New(db).GetMessage(ctx, schema.GetMessageParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
	})
	require.NoError(t, err)
	require.Equal(t, 1, msg.Attrs.Reactions["thumbsup"])

	err = schema.New(db).UpdateReaction(ctx, schema.UpdateReactionParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
		Reaction:  "thumbsup",
		Count:     1,
	})
	require.NoError(t, err)

	msg, err = schema.New(db).GetMessage(ctx, schema.GetMessageParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
	})
	require.NoError(t, err)
	require.Equal(t, 2, msg.Attrs.Reactions["thumbsup"])

	err = schema.New(db).UpdateReaction(ctx, schema.UpdateReactionParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
		Reaction:  "thumbsup",
		Count:     -1,
	})
	require.NoError(t, err)

	msg, err = schema.New(db).GetMessage(ctx, schema.GetMessageParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
	})
	require.NoError(t, err)
	require.Equal(t, 1, msg.Attrs.Reactions["thumbsup"])

	err = schema.New(db).UpdateReaction(ctx, schema.UpdateReactionParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
		Reaction:  "thumbsup",
		Count:     -1,
	})
	require.NoError(t, err)

	msg, err = schema.New(db).GetMessage(ctx, schema.GetMessageParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
	})
	require.NoError(t, err)
	require.Empty(t, msg.Attrs.Reactions)

	err = schema.New(db).UpdateReaction(ctx, schema.UpdateReactionParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
		Reaction:  "thumbsup",
		Count:     -1,
	})
	require.NoError(t, err)

	msg, err = schema.New(db).GetMessage(ctx, schema.GetMessageParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
	})
	require.NoError(t, err)
	require.Empty(t, msg.Attrs.Reactions)
}
