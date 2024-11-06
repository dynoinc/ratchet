package internal

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/robfig/cron/v3"

	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

var (
	ErrChannelNotKnown = errors.New("channel not known")
)

type Bot struct {
	DB             *pgxpool.Pool
	RiverClient    *river.Client[pgx.Tx]
	lookbackPeriod time.Duration
}

func New(db *pgxpool.Pool) *Bot {
	return &Bot{
		DB:             db,
		lookbackPeriod: background.DefaultHistoricalLookbackPeriod,
	}
}

// Initialize should be called after RiverClient is set
func (b *Bot) Initialize() error {
	if b.RiverClient == nil {
		return fmt.Errorf("RiverClient must be set before initialization")
	}

	// Setup periodic jobs
	if err := b.SetupWeeklyReportJob(); err != nil {
		return fmt.Errorf("failed to setup weekly report job: %w", err)
	}

	return nil
}

/* Slack channels related methods */

func (b *Bot) AddChannel(ctx context.Context, channelID string) error {
	if _, err := schema.New(b.DB).AddChannel(ctx, channelID); err != nil {
		return err
	}

	// Schedule historical message ingestion
	now := time.Now()
	_, err := b.RiverClient.Insert(
		ctx,
		background.MessagesIngestionWorkerArgs{
			ChannelID: channelID,
			StartTime: now.Add(-b.lookbackPeriod),
			EndTime:   now,
		},
		nil,
	)
	return err
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

	if _, err = b.RiverClient.Insert(
		ctx,
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

func (b *Bot) CloseIncident(ctx context.Context, alert string, service string, endTimestamp time.Time) error {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := schema.New(b.DB).WithTx(tx)
	incidentID, err := qtx.FindActiveIncident(ctx, schema.FindActiveIncidentParams{
		Alert:   alert,
		Service: service,
	})
	if err != nil {
		return err
	}

	if _, err := qtx.CloseIncident(ctx, schema.CloseIncidentParams{
		EndTimestamp: pgtype.Timestamptz{
			Time:  endTimestamp,
			Valid: true,
		},
		IncidentID: incidentID,
	}); err != nil {
		return err
	}

	// TODO: enqueue a background job to process this incident.

	return tx.Commit(ctx)
}

// SetupWeeklyReportJob configures the weekly report periodic job
func (b *Bot) SetupWeeklyReportJob() error {
	if b.RiverClient == nil {
		return fmt.Errorf("RiverClient is not initialized")
	}

	// Schedule for every Monday at 9 AM PST
	schedule, err := cron.ParseStandard("0 9 * * 1")
	if err != nil {
		return fmt.Errorf("error parsing cron schedule: %w", err)
	}

	constructor := func() (river.JobArgs, *river.InsertOpts) {
		return &background.WeeklyReportJobArgs{}, nil
	}

	periodicJob := river.NewPeriodicJob(schedule, constructor, &river.PeriodicJobOpts{
		RunOnStart: false,
	})

	b.RiverClient.PeriodicJobs().Add(periodicJob)
	return nil
}
