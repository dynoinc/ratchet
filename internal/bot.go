package internal

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

var (
	ErrChannelNotKnown = errors.New("channel not known")
)

type Bot struct {
	db          *pgxpool.Pool
	queries     *schema.Queries
	riverClient *river.Client[pgx.Tx]
}

func New(db *pgxpool.Pool, riverClient *river.Client[pgx.Tx]) (*Bot, error) {
	return &Bot{
		db:          db,
		queries:     schema.New(db),
		riverClient: riverClient,
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
	slackTs string,
	attrs dto.MessageAttrs,
) error {
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := b.queries.WithTx(tx)
	if err := qtx.AddMessage(ctx, schema.AddMessageParams{
		ChannelID: channelID,
		SlackTs:   slackTs,
		Attrs:     attrs,
	}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.ForeignKeyViolation {
			return ErrChannelNotKnown
		}

		return err
	}

	if _, err = b.riverClient.Insert(
		ctx,
		background.ClassifyMessageArgs{ChannelID: channelID, SlackTS: slackTs},
		nil,
	); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (b *Bot) IsChannelEnabled(ctx context.Context, channelID string) (bool, error) {
	channel, err := b.queries.GetChannelByID(ctx, channelID)
	if err != nil {
		return false, fmt.Errorf("failed to get channel: %w", err)
	}
	return channel.Enabled, nil
}
