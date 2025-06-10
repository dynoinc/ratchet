package deployment_ops

import (
	"context"
	"fmt"

	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/slack-go/slack"
)

func Post(
	ctx context.Context,
	qtx *schema.Queries,
	llmClient llm.Client,
	slackIntegration slack_integration.Integration,
	channelID string,
	text string,
) error {
	response, err := llmClient.ProccessDeploymentOps(ctx, text)
	if err != nil {
		return fmt.Errorf("processing deployment ops query: %w", err)
	}

	// Create response blocks for Slack
	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, response, false, false),
			nil,
			nil,
		),
	}
	blocks = append(blocks, slack_integration.CreateSignatureBlock("Deployment Ops")...)

	return slackIntegration.PostMessage(ctx, channelID, blocks...)
}
