package background

import (
	"context"
	"fmt"

	"github.com/riverqueue/river"
)

type ClassifyMessageArgs struct {
	ChannelID string `json:"channel_id"`
	SlackTS   string `json:"slack_ts"`
}

func (c ClassifyMessageArgs) Kind() string {
	return "classify_message"
}

type ClassifyMessageWorker struct {
	river.WorkerDefaults[ClassifyMessageArgs]
}

func (w *ClassifyMessageWorker) Work(ctx context.Context, job *river.Job[ClassifyMessageArgs]) error {
	fmt.Printf("Classifying: %+v\n", job.Args)
	return nil
}
