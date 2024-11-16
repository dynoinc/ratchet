package internal

import (
	"context"
	"errors"
	"fmt"

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
	RiverClient *river.Client[pgx.Tx]
}

func New(db *pgxpool.Pool) *Bot {
	return &Bot{
		DB: db,
	}
}

/* Slack channels related methods */

func (b *Bot) AddChannel(ctx context.Context, channelID string) (schema.Channel, error) {
	channel, err := schema.New(b.DB).AddChannel(ctx, channelID)
	if err != nil {
		return schema.Channel{}, fmt.Errorf("error adding channel: %w", err)
	}

	return channel, nil
}

func (b *Bot) GetChannel(ctx context.Context, channelID string) (schema.Channel, error) {
	return schema.New(b.DB).GetChannel(ctx, channelID)
}

/* Slack messages related methods */

func (b *Bot) Notify(ctx context.Context, channelID string) error {
	channel, err := b.AddChannel(ctx, channelID)
	if err != nil {
		return fmt.Errorf("error adding channel: %w", err)
	}

	// Trigger ingestion job now to process the message.
	if _, err := b.RiverClient.Insert(
		ctx,
		background.MessagesIngestionWorkerArgs{
			ChannelID:     channelID,
			OldestSlackTS: channel.LatestSlackTs,
		},
		&river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}},
	); err != nil {
		return err
	}

	return nil
}

func (b *Bot) AddMessages(ctx context.Context, channelID string, messages []slack.Message) error {
	if len(messages) == 0 {
		return nil
	}

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
				return ErrChannelNotKnown
			}

			return err
		}

		jobs = append(jobs, river.InsertManyParams{
			Args: background.ClassifierArgs{ChannelID: channelID, SlackTS: message.Timestamp},
		})
	}

	if _, err = b.RiverClient.InsertManyTx(ctx, tx, jobs); err != nil {
		return err
	}

	if err := qtx.UpdateLatestSlackTs(ctx, schema.UpdateLatestSlackTsParams{
		ChannelID:     channelID,
		LatestSlackTs: messages[0].Timestamp,
	}); err != nil {
		return err
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
