package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/slack-go/slack"

	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

var (
	ErrChannelNotKnown = errors.New("channel not known")
	ErrNoOpenIncident  = errors.New("no open incident found")
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

func (b *Bot) Init(ctx context.Context, riverClient *river.Client[pgx.Tx]) error {
	b.riverClient = riverClient
	return nil
}

// AddChannel adds a channel to the database. Primarily used for testing.
func (b *Bot) AddChannel(ctx context.Context, channelID string) (schema.Channel, error) {
	channel, err := schema.New(b.DB).AddChannel(ctx, schema.AddChannelParams{
		ChannelID: channelID,
	})
	if err != nil {
		return schema.Channel{}, fmt.Errorf("error adding channel: %w", err)
	}

	return channel, nil
}

func (b *Bot) Notify(ctx context.Context, channelID string) error {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := schema.New(b.DB).WithTx(tx)
	channel, err := qtx.AddChannel(ctx, schema.AddChannelParams{
		ChannelID: channelID,
	})
	if err != nil {
		return fmt.Errorf("error adding channel: %w", err)
	}

	// If this is a new channel without attributes, schedule a job to fetch info
	if channel.Attrs == (dto.ChannelAttrs{}) || channel.Attrs.Name == "" {
		_, err = b.riverClient.InsertTx(ctx, tx, background.ChannelInfoWorkerArgs{
			ChannelID: channelID,
		}, &river.InsertOpts{
			UniqueOpts: river.UniqueOpts{ByArgs: false},
		})
		if err != nil {
			return fmt.Errorf("error scheduling channel info fetch: %w", err)
		}
	}

	_, err = b.riverClient.InsertTx(
		ctx,
		tx,
		background.MessagesIngestionWorkerArgs{
			ChannelID:        channelID,
			SlackTSWatermark: channel.SlackTsWatermark,
		},
		&river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}},
	)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (b *Bot) AddMessages(ctx context.Context, channelID string, messages []slack.Message, newWatermark string) error {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := schema.New(b.DB).WithTx(tx)
	var jobs []river.InsertManyParams
	for _, message := range messages {
		if err := qtx.AddMessage(ctx, schema.AddMessageParams{
			ChannelID: channelID,
			SlackTs:   message.Timestamp,
			Attrs:     dto.MessageAttrs{Message: &message},
		}); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.ForeignKeyViolation {
				return fmt.Errorf("error adding message to %s: %w", channelID, ErrChannelNotKnown)
			}

			return err
		}

		jobs = append(jobs, river.InsertManyParams{
			Args: background.ClassifierArgs{ChannelID: channelID, SlackTS: message.Timestamp},
		})
	}

	if len(jobs) > 0 {
		if _, err = b.riverClient.InsertManyTx(ctx, tx, jobs); err != nil {
			return err
		}
	}

	if err := qtx.UpdateSlackTSWatermark(ctx, schema.UpdateSlackTSWatermarkParams{
		ChannelID:        channelID,
		SlackTsWatermark: newWatermark,
	}); err != nil {
		return err
	}

	// If channel had activity, there is a high chance there is more to ingest.
	scheduledAt := time.Time{}
	if len(messages) == 0 {
		scheduledAt = time.Now().Add(time.Minute)
	}

	if _, err := b.riverClient.InsertTx(
		ctx,
		tx,
		background.MessagesIngestionWorkerArgs{
			ChannelID:        channelID,
			SlackTSWatermark: newWatermark,
		},
		&river.InsertOpts{
			UniqueOpts:  river.UniqueOpts{ByArgs: true},
			ScheduledAt: scheduledAt,
		},
	); err != nil {
		return err
	}

	// If we don't have any attributes, add a job to fetch them
	channel, err := qtx.GetChannel(ctx, channelID)
	if err != nil {
		return err
	}

	if channel.Attrs == (dto.ChannelAttrs{}) || channel.Attrs.Name == "" {
		_, err = b.riverClient.InsertTx(ctx, tx, background.ChannelInfoWorkerArgs{
			ChannelID: channelID,
		}, &river.InsertOpts{
			UniqueOpts: river.UniqueOpts{ByArgs: false},
		})
		// Suppress error, we don't want to block from adding messages if we can't schedule a job
		// This is a best effort to fetch channel info.
		if err != nil {
			slog.Error("error adding channel info worker",
				"channel_id", channelID,
				"error", err)
		}
	}

	return tx.Commit(ctx)
}

func (b *Bot) TagAsBotNotification(ctx context.Context, channelID, slackTs, botName string) error {
	return schema.New(b.DB).TagAsBotNotification(ctx, schema.TagAsBotNotificationParams{
		ChannelID: channelID,
		SlackTs:   slackTs,
		BotName:   botName,
	})
}

func (b *Bot) TagAsUserMessage(ctx context.Context, channelID, slackTs, userID string) error {
	return schema.New(b.DB).TagAsUserMessage(ctx, schema.TagAsUserMessageParams{
		ChannelID: channelID,
		SlackTs:   slackTs,
		UserID:    userID,
	})
}

func (b *Bot) GetMessage(ctx context.Context, channelID string, slackTs string) (schema.Message, error) {
	return schema.New(b.DB).GetMessage(ctx, schema.GetMessageParams{
		ChannelID: channelID,
		SlackTs:   slackTs,
	})
}

/* Incident related methods */

func (b *Bot) OpenIncident(ctx context.Context, params schema.OpenIncidentParams) (int32, error) {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := schema.New(b.DB).WithTx(tx)
	id, err := qtx.OpenIncident(ctx, params)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return id, nil
		}

		return 0, err
	}

	if err := qtx.SetIncidentID(ctx, schema.SetIncidentIDParams{
		ChannelID:  params.ChannelID,
		SlackTs:    params.SlackTs,
		IncidentID: id,
		Action:     "open",
	}); err != nil {
		return 0, err
	}

	// TODO: enqueue a background job to post runbook for the alert to slack if we have any.

	return 0, tx.Commit(ctx)
}

func (b *Bot) CloseIncident(
	ctx context.Context,
	channelID, slackTs, alert, service string,
	endTimestamp pgtype.Timestamptz,
) error {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := schema.New(b.DB).WithTx(tx)
	incident, err := qtx.GetLatestIncidentBeforeTimestamp(ctx, schema.GetLatestIncidentBeforeTimestampParams{
		ChannelID:       channelID,
		Alert:           alert,
		Service:         service,
		BeforeTimestamp: endTimestamp,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoOpenIncident
		}

		return err
	}

	if err := qtx.SetIncidentID(ctx, schema.SetIncidentIDParams{
		ChannelID:  channelID,
		SlackTs:    slackTs,
		IncidentID: incident.IncidentID,
		Action:     "close",
	}); err != nil {
		return err
	}

	if _, err := qtx.CloseIncident(ctx, schema.CloseIncidentParams{
		EndTimestamp: endTimestamp,
		IncidentID:   incident.IncidentID,
	}); err != nil {
		return err
	}

	// TODO: enqueue a background job to process this incident.

	return tx.Commit(ctx)
}
