package internal

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type Bot struct {
	db      *pgxpool.Pool
	queries *schema.Queries
}

func New(db *pgxpool.Pool) (*Bot, error) {
	return &Bot{
		db:      db,
		queries: schema.New(db),
	}, nil
}

/* Slack channels related methods */

func (b *Bot) InsertOrEnableChannel(ctx context.Context, channelID string) error {
	_, err := b.queries.InsertOrEnableChannel(ctx, channelID)
	return err
}

func (b *Bot) DisableChannel(ctx context.Context, channel string) error {
	_, err := b.queries.DisableSlackChannel(ctx, channel)
	return err
}

/* Slack messages related methods */

func (b *Bot) AddMessage(
	ctx context.Context,
	channelID string,
	threadTs string,
	attrs dto.MessageAttrs,
) error {
	err := b.queries.AddMessage(ctx, schema.AddMessageParams{
		ChannelID: channelID,
		SlackTs:   threadTs,
		Attrs:     attrs,
	})

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		// Channel not found, insert channel and retry.
		if err := b.InsertOrEnableChannel(ctx, channelID); err != nil {
			return err
		}

		return b.AddMessage(ctx, channelID, threadTs, attrs)
	}

	return err
}
