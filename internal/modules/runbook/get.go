package runbook

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/modules/recent_activity"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/jackc/pgx/v5"
	"github.com/slack-go/slack"
)

func Get(
	ctx context.Context,
	qtx *schema.Queries,
	llmClient llm.Client,
	serviceName, alertName string,
	botID string,
) ([]slack.Block, error) {
	runbook, err := qtx.GetRunbook(ctx, schema.GetRunbookParams{
		ServiceName: serviceName,
		AlertName:   alertName,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("getting runbook: %w", err)
	}

	runbookMessage := runbook.Attrs.Runbook
	if runbookMessage == "" {
		runbookMessage, err = Update(ctx, qtx, llmClient, serviceName, alertName, false)
		if err != nil {
			return nil, fmt.Errorf("updating runbook: %w", err)
		}
	}

	if runbookMessage == "" {
		return nil, nil
	}

	updates, err := recent_activity.Get(ctx, qtx, llmClient, serviceName, alertName, time.Hour, botID)
	if err != nil {
		return nil, fmt.Errorf("getting updates: %w", err)
	}

	blocks := format(serviceName, alertName, runbookMessage, updates)
	return blocks, nil
}

func format(
	serviceName, alertName, runbookMessage string,
	updates []recent_activity.Activity,
) []slack.Block {
	// Create blocks array and add header
	blocks := []slack.Block{
		slack.NewHeaderBlock(
			slack.NewTextBlockObject(slack.PlainTextType, fmt.Sprintf("Runbook for %s - %s", serviceName, alertName), false, false),
		),
		slack.NewDividerBlock(),
	}

	// Add runbook content
	if len(runbookMessage) > 0 {
		blocks = append(blocks,
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "*Runbook:*", false, false),
				nil, nil,
			),
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, runbookMessage, false, false),
				nil, nil,
			),
		)
	} else {
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, "_No runbook found for this alert_", false, false),
			nil, nil,
		))
	}

	// Add divider before updates section
	blocks = append(blocks, slack.NewDividerBlock())

	if len(updates) > 0 {
		blocks = append(blocks,
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "*Recent Activity:*", false, false),
				nil, nil,
			),
		)

		for _, update := range updates {
			messageLink := fmt.Sprintf("https://slack.com/app_redirect?channel=%s&message_ts=%s",
				update.ChannelID, update.Ts)
			updateText := fmt.Sprintf("• <%s|%s> (%s)",
				messageLink, update.Attrs.Message.Text, update.Attrs.Message.User)

			blocks = append(blocks, slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, updateText, false, false),
				nil, nil,
			))
		}
	} else {
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, "_No recent activity found in the last hour_", false, false),
			nil, nil,
		))
	}

	// Add divider before footer
	blocks = append(blocks, slack.NewDividerBlock())

	// Add footer
	blocks = append(blocks, slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType,
			fmt.Sprintf("_Generated by Ratchet at %s_", time.Now().Format(time.RFC1123)),
			false, false,
		),
		nil, nil,
	))

	return blocks
}
