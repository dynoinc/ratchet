package runbook

import (
	"context"

	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/slack-go/slack"
)

func Post(ctx context.Context, slackIntegration *slack_integration.Integration, channelID, slackTS string, blocks ...slack.Block) error {
	return slackIntegration.PostThreadReply(ctx, channelID, slackTS, blocks...)
}
