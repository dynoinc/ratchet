package runbook

import (
	"context"
	"fmt"

	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

func Get(
	ctx context.Context,
	qtx *schema.Queries,
	llmClient llm.Client,
	serviceName, alertName string,
	botID string,
) (*llm.RunbookResponse, error) {
	msgs, err := qtx.GetThreadMessagesByServiceAndAlert(ctx, schema.GetThreadMessagesByServiceAndAlertParams{
		Service: serviceName,
		Alert:   alertName,
		BotID:   botID,
	})
	if err != nil {
		return nil, fmt.Errorf("getting thread messages: %w", err)
	}

	if len(msgs) == 0 {
		return nil, nil
	}

	runbookMessage, err := llmClient.CreateRunbook(ctx, serviceName, alertName, msgs)
	if err != nil {
		return nil, fmt.Errorf("creating runbook: %w", err)
	}

	return runbookMessage, nil
}
