package channel_info_worker

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/slack-go/slack"

	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type ChannelInfoWorker struct {
	river.WorkerDefaults[background.ChannelInfoWorkerArgs]

	slackClient *slack.Client
	db          *pgxpool.Pool
}

func New(slackClient *slack.Client, db *pgxpool.Pool) *ChannelInfoWorker {
	return &ChannelInfoWorker{
		slackClient: slackClient,
		db:          db,
	}
}

func (w *ChannelInfoWorker) Work(ctx context.Context, job *river.Job[background.ChannelInfoWorkerArgs]) error {
	channelInfo, err := w.slackClient.GetConversationInfo(&slack.GetConversationInfoInput{
		ChannelID: job.Args.ChannelID,
	})
	if err != nil {
		return fmt.Errorf("error getting channel info for channel ID %s: %w", job.Args.ChannelID, err)
	}

	attrs := dto.ChannelAttrs{
		Name: channelInfo.Name,
	}

	if err = schema.New(w.db).UpdateChannelAttrs(ctx, schema.UpdateChannelAttrsParams{
		ChannelID: job.Args.ChannelID,
		Attrs:     attrs,
	}); err != nil {
		return fmt.Errorf("error updating channel info for channel ID %s: %w", job.Args.ChannelID, err)
	}

	return nil
}
