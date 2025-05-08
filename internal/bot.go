package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/slack-go/slack/slackevents"

	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/docs"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type messageSource int

const (
	sourceSlack messageSource = iota
	SourceBackfill
)

var (
	ErrMessageNotFound = errors.New("message not found")
)

type Bot struct {
	DB          *pgxpool.Pool
	DocsConfig  *docs.Config
	RiverClient *river.Client[pgx.Tx]
}

func New(db *pgxpool.Pool) *Bot {
	return &Bot{DB: db}
}

func (b *Bot) Init(riverClient *river.Client[pgx.Tx], docsConfig *docs.Config) error {
	b.RiverClient = riverClient
	b.DocsConfig = docsConfig
	return nil
}

func (b *Bot) UpdateChannel(ctx context.Context, tx pgx.Tx, params schema.UpdateChannelAttrsParams) error {
	qtx := schema.New(b.DB)
	if tx != nil {
		qtx = qtx.WithTx(tx)
	}

	if err := qtx.UpdateChannelAttrs(ctx, params); err != nil {
		return fmt.Errorf("updating channel %s: %w", params.ID, err)
	}

	return nil
}

func (b *Bot) EnableAutoDocReply(ctx context.Context, channelID string) error {
	return b.UpdateChannel(ctx, nil, schema.UpdateChannelAttrsParams{
		ID:    channelID,
		Attrs: dto.ChannelAttrs{DocResponsesEnabled: true},
	})
}

func (b *Bot) DisableAutoDocReply(ctx context.Context, channelID string) error {
	return b.UpdateChannel(ctx, nil, schema.UpdateChannelAttrsParams{
		ID:    channelID,
		Attrs: dto.ChannelAttrs{DocResponsesEnabled: false},
	})
}

func (b *Bot) AddMessage(ctx context.Context, tx pgx.Tx, params []schema.AddMessageParams, source messageSource) error {
	qtx := schema.New(b.DB).WithTx(tx)

	channelID := params[0].ChannelID
	channel, err := qtx.AddChannel(ctx, channelID)
	if err != nil {
		return fmt.Errorf("adding channel %s: %w", channelID, err)
	}

	if channel.Attrs == (dto.ChannelAttrs{}) {
		if err := qtx.UpdateChannelAttrs(ctx, schema.UpdateChannelAttrsParams{
			ID: channelID,
			Attrs: dto.ChannelAttrs{
				OnboardingStatus: dto.OnboardingStatusStarted,
			},
		}); err != nil {
			return fmt.Errorf("updating channel %s: %w", channelID, err)
		}

		if _, err := b.RiverClient.InsertTx(ctx, tx, background.ChannelOnboardWorkerArgs{
			ChannelID: channelID,
		}, nil); err != nil {
			return fmt.Errorf("scheduling channel onboarding for channel %s: %w", channelID, err)
		}
	}

	// Delete old messages
	if err := qtx.DeleteOldMessages(ctx, schema.DeleteOldMessagesParams{
		ChannelID: channelID,
		OlderThan: pgtype.Interval{Days: 2 * 365, Valid: true},
	}); err != nil {
		return fmt.Errorf("deleting old messages for channel %s: %w", channelID, err)
	}

	var jobs []river.InsertManyParams
	for _, param := range params {
		if err := qtx.AddMessage(ctx, param); err != nil {
			return fmt.Errorf("adding message (ts=%s) to channel %s: %w", param.Ts, param.ChannelID, err)
		}

		var insertOpts *river.InsertOpts
		if source == SourceBackfill {
			insertOpts = &river.InsertOpts{
				// Avoid overloading the worker with backfill jobs
				Priority: 4,
			}
		}

		jobs = append(jobs, river.InsertManyParams{
			Args: background.ModulesWorkerArgs{
				ChannelID:  param.ChannelID,
				SlackTS:    param.Ts,
				IsBackfill: source == SourceBackfill,
			},
			InsertOpts: insertOpts,
		})
	}

	if _, err := b.RiverClient.InsertManyTx(ctx, tx, jobs); err != nil {
		return fmt.Errorf("scheduling message classification for channel %s: %w", channelID, err)
	}

	return nil
}

func (b *Bot) AddThreadMessages(ctx context.Context, tx pgx.Tx, params []schema.AddThreadMessageParams, source messageSource) error {
	qtx := schema.New(b.DB).WithTx(tx)

	var jobs []river.InsertManyParams
	for _, param := range params {
		if err := qtx.AddThreadMessage(ctx, param); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.ForeignKeyViolation {
				continue
			}

			return fmt.Errorf("adding thread message to channel %s (ts=%s): %w", param.ChannelID, param.Ts, err)
		}

		jobs = append(jobs, river.InsertManyParams{
			Args: background.ModulesWorkerArgs{
				ChannelID:  param.ChannelID,
				SlackTS:    param.Ts,
				ParentTS:   param.ParentTs,
				IsBackfill: source == SourceBackfill,
			},
		})
	}

	if len(jobs) > 0 {
		if _, err := b.RiverClient.InsertManyTx(ctx, tx, jobs); err != nil {
			return fmt.Errorf("scheduling thread message backfill for channel %s: %w", params[0].ChannelID, err)
		}
	}

	return nil
}

func (b *Bot) NotifyMessage(ctx context.Context, ev *slackevents.MessageEvent) error {
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
		}, sourceSlack); err != nil {
			return fmt.Errorf("adding message: %w", err)
		}
	} else {
		if err := b.AddThreadMessages(ctx, tx, []schema.AddThreadMessageParams{
			{
				ChannelID: ev.Channel,
				ParentTs:  ev.ThreadTimeStamp,
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
		}, sourceSlack); err != nil {
			return fmt.Errorf("adding thread message: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (b *Bot) updateReaction(ctx context.Context, item slackevents.Item, reaction string, count int) error {
	slog.DebugContext(ctx, "updating reaction", "item", item, "reaction", reaction, "count", count)
	if item.Type != "message" {
		return nil
	}

	if err := schema.New(b.DB).UpdateReaction(ctx, schema.UpdateReactionParams{
		ChannelID: item.Channel,
		Ts:        item.Timestamp,
		Reaction:  reaction,
		Count:     int32(count),
	}); err != nil {
		return fmt.Errorf("updating reaction: %w", err)
	}

	return nil
}

func (b *Bot) NotifyReactionRemoved(ctx context.Context, ev *slackevents.ReactionRemovedEvent) error {
	return b.updateReaction(ctx, ev.Item, ev.Reaction, -1)
}

func (b *Bot) NotifyReactionAdded(ctx context.Context, ev *slackevents.ReactionAddedEvent) error {
	return b.updateReaction(ctx, ev.Item, ev.Reaction, 1)
}

func (b *Bot) GetMessage(
	ctx context.Context,
	channelID string,
	slackTs string,
) (schema.GetMessageRow, error) {
	msg, err := schema.New(b.DB).GetMessage(ctx, schema.GetMessageParams{
		ChannelID: channelID,
		Ts:        slackTs,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return schema.GetMessageRow{}, fmt.Errorf("message not found (ts=%s) from channel %s: %w", slackTs, channelID, ErrMessageNotFound)
		}

		return schema.GetMessageRow{}, fmt.Errorf("getting message (ts=%s) from channel %s: %w", slackTs, channelID, err)
	}

	return msg, nil
}
