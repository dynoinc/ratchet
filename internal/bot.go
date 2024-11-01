package internal

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dynoinc/ratchet/internal/storage/schema"
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

func (b *Bot) InsertIntent(ctx context.Context, channelID string) (bool, error) {
	slackChannel, err := b.queries.InsertOrGetSlackChannel(ctx, channelID)
	if err != nil {
		return false, err
	}

	return !slackChannel.Enabled, nil
}

func (b *Bot) OnboardChannel(ctx context.Context, channelID string, teamName string) error {
	tx, err := b.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

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
