package internal

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/slack-go/slack/slackevents"

	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

var (
	ErrMessageNotFound = errors.New("message not found")
)

type Bot struct {
	DB          *pgxpool.Pool
	riverClient *river.Client[pgx.Tx]
}

func New(db *pgxpool.Pool) *Bot {
	return &Bot{
		DB: db,
	}
}

func (b *Bot) Init(riverClient *river.Client[pgx.Tx]) error {
	b.riverClient = riverClient
	return nil
}

func (b *Bot) UpdateChannel(ctx context.Context, tx pgx.Tx, params schema.UpdateChannelAttrsParams) error {
	qtx := schema.New(b.DB).WithTx(tx)

	if err := qtx.UpdateChannelAttrs(ctx, params); err != nil {
		return fmt.Errorf("updating channel %s: %w", params.ID, err)
	}

	return nil
}

func (b *Bot) AddMessage(ctx context.Context, tx pgx.Tx, params []schema.AddMessageParams) error {
	qtx := schema.New(b.DB).WithTx(tx)

	channelID := params[0].ChannelID
	channel, err := qtx.AddChannel(ctx, channelID)
	if err != nil {
		return fmt.Errorf("adding channel %s: %w", channelID, err)
	}

	if channel.Attrs == (dto.ChannelAttrs{}) {
		if _, err := b.riverClient.InsertTx(ctx, tx, background.ChannelOnboardWorkerArgs{
			ChannelID: channelID,
		}, nil); err != nil {
			return fmt.Errorf("scheduling channel onboarding for channel %s: %w", channelID, err)
		}
	}

	var jobs []river.InsertManyParams
	for _, param := range params {
		if err := qtx.AddMessage(ctx, param); err != nil {
			return fmt.Errorf("adding message (ts=%s) to channel %s: %w", param.Ts, param.ChannelID, err)
		}

		jobs = append(jobs, river.InsertManyParams{
			Args: background.ClassifierArgs{ChannelID: param.ChannelID, SlackTS: param.Ts},
		})
	}

	if _, err := b.riverClient.InsertManyTx(ctx, tx, jobs); err != nil {
		return fmt.Errorf("scheduling message classification for channel %s: %w", channelID, err)
	}

	return nil
}

func (b *Bot) AddThreadMessage(ctx context.Context, tx pgx.Tx, params schema.AddThreadMessageParams) error {
	qtx := schema.New(b.DB).WithTx(tx)

	// TODO: handle parent message not found
	qtx.AddThreadMessage(ctx, params)
	return nil
}

func (b *Bot) Notify(ctx context.Context, ev *slackevents.MessageEvent) error {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if ev.ThreadTimeStamp == "" {
		if err := b.AddMessage(ctx, tx, []schema.AddMessageParams{
			{
				ChannelID: ev.Channel,
				Ts:        ev.TimeStamp,
				Attrs: dto.MessageAttrs{
					Message: dto.SlackMessage{
						SubType:     ev.SubType,
						Text:        ev.Text,
						User:        ev.User,
						BotID:       ev.BotID,
						BotUsername: ev.Username,
					},
				},
			},
		}); err != nil {
			return fmt.Errorf("adding message: %w", err)
		}
	} else {
		if err := b.AddThreadMessage(ctx, tx, schema.AddThreadMessageParams{
			ChannelID: ev.Channel,
			ParentTs:  ev.ThreadTimeStamp,
			Ts:        ev.TimeStamp,
			Attrs: dto.ThreadMessageAttrs{
				Message: dto.SlackMessage{
					SubType:     ev.SubType,
					Text:        ev.Text,
					User:        ev.User,
					BotID:       ev.BotID,
					BotUsername: ev.Username,
				},
			},
		}); err != nil {
			return fmt.Errorf("adding thread message: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (b *Bot) GetMessage(ctx context.Context, channelID string, slackTs string) (dto.MessageAttrs, error) {
	msg, err := schema.New(b.DB).GetMessage(ctx, schema.GetMessageParams{
		ChannelID: channelID,
		Ts:        slackTs,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return dto.MessageAttrs{}, fmt.Errorf("message not found (ts=%s) from channel %s: %w", slackTs, channelID, ErrMessageNotFound)
		}

		return dto.MessageAttrs{}, fmt.Errorf("getting message (ts=%s) from channel %s: %w", slackTs, channelID, err)
	}

	return msg.Attrs, nil
}
