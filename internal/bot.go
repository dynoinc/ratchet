package internal

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

const (
	defaultHistoricalLookbackPeriod = 14 * 24 * time.Hour // 2 weeks
)

var (
	ErrChannelNotKnown = errors.New("channel not known")
	ErrNoOpenIncident  = errors.New("no open incident found")
)

type Bot struct {
	DB          *pgxpool.Pool
	RiverClient *river.Client[pgx.Tx]

	lookbackPeriod time.Duration
}

func New(db *pgxpool.Pool) *Bot {
	return &Bot{
		DB:             db,
		lookbackPeriod: defaultHistoricalLookbackPeriod,
	}
}

/* Slack channels related methods */

func (b *Bot) AddChannel(ctx context.Context, channelID string) error {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := schema.New(b.DB).WithTx(tx)
	if _, err := qtx.AddChannel(ctx, channelID); err != nil {
		return err
	}

	// Schedule historical message ingestion
	now := time.Now()
	if _, err := b.RiverClient.InsertTx(
		ctx,
		tx,
		background.MessagesIngestionWorkerArgs{
			ChannelID: channelID,
			StartTime: now.Add(-b.lookbackPeriod),
			EndTime:   now,
		},
		nil,
	); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

/* Slack messages related methods */

func (b *Bot) AddMessage(
	ctx context.Context,
	channelID string,
	slackTs string,
	attrs dto.MessageAttrs,
) error {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := schema.New(b.DB).WithTx(tx)
	if err := qtx.AddMessage(ctx, schema.AddMessageParams{
		ChannelID: channelID,
		SlackTs:   slackTs,
		Attrs:     attrs,
	}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.ForeignKeyViolation {
			return ErrChannelNotKnown
		}

		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			// already exists, ignore
			return nil
		}

		return err
	}

	if _, err = b.RiverClient.InsertTx(
		ctx,
		tx,
		background.ClassifierArgs{ChannelID: channelID, SlackTS: slackTs},
		nil,
	); err != nil {
		return err
	}

	return tx.Commit(ctx)
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
	}); err != nil {
		return 0, err
	}

	// TODO: enqueue a background job to post runbook for the alert to slack if we have any.

	return 0, tx.Commit(ctx)
}

func (b *Bot) CloseIncident(
	ctx context.Context,
	channelID, alert, service string,
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

	if _, err := qtx.CloseIncident(ctx, schema.CloseIncidentParams{
		EndTimestamp: endTimestamp,
		IncidentID:   incident.IncidentID,
	}); err != nil {
		return err
	}

	// TODO: enqueue a background job to process this incident.

	return tx.Commit(ctx)
}
