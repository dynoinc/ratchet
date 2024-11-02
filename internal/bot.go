package internal

import (
	"context"
	"errors"
	"fmt"

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

func (b *Bot) InsertIntent(ctx context.Context, channelID string) (bool, error) {
	slackChannel, err := b.queries.InsertOrGetSlackChannel(ctx, channelID)
	if err != nil {
		return false, err
	}

	return slackChannel.Enabled, nil
}

func (b *Bot) OnboardChannel(ctx context.Context, channelID string, teamName string) error {
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := b.queries.WithTx(tx)
	existingRecord, err := qtx.GetSlackChannelByID(ctx, channelID)
	if err != nil {
		return err
	}
	if existingRecord.Enabled {
		return fmt.Errorf("channel %s is already onboarded", channelID)
	}

	if _, err = qtx.UpdateSlackChannel(ctx, schema.UpdateSlackChannelParams{
		ChannelID: channelID,
		TeamName:  teamName,
	}); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (b *Bot) DisableChannel(ctx context.Context, channel string) error {
	_, err := b.queries.DisableSlackChannel(ctx, channel)
	return err
}

/* Slack conversations related methods */

func (b *Bot) StartConversation(ctx context.Context, channelID string, conversationID string, attrs dto.MessageAttrs) (bool, error) {
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := b.queries.WithTx(tx)

	if err := qtx.StartConversation(ctx, schema.StartConversationParams{
		ChannelID: channelID,
		SlackTs:   conversationID,
	}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			// Another conversation has already been added with this ID.
			return false, nil
		}

		return false, err
	}

	if err := qtx.AddMessage(ctx, schema.AddMessageParams{
		ChannelID: channelID,
		SlackTs:   conversationID,
		MessageTs: conversationID,
		Attrs:     attrs,
	}); err != nil {
		return false, err
	}

	if err = tx.Commit(ctx); err != nil {
		return false, err
	}

	return true, nil
}

func (b *Bot) AddMessage(
	ctx context.Context,
	channelID string,
	threadTs string,
	messageTs string,
	attrs dto.MessageAttrs,
) error {
	err := b.queries.AddMessage(ctx, schema.AddMessageParams{
		ChannelID: channelID,
		SlackTs:   threadTs,
		MessageTs: messageTs,
		Attrs:     attrs,
	})

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		// Conversation not found. Ignore this error.
		return nil
	}

	return err
}
