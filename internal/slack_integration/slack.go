package slack_integration

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/dynoinc/ratchet/internal"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

type integration struct {
	BotUserID string
	client    *socketmode.Client

	bot *internal.Bot
}

func New(ctx context.Context, appToken, botToken string, bot *internal.Bot) (*integration, error) {
	api := slack.New(botToken, slack.OptionAppLevelToken(appToken))

	authTest, err := api.AuthTestContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("slack API test failed: %w", err)
	}

	socketClient := socketmode.New(api)

	return &integration{
		BotUserID: authTest.UserID,
		client:    socketClient,
		bot:       bot,
	}, nil
}

func (b *integration) Run(ctx context.Context) error {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-b.client.Events:
				switch evt.Type {
				case socketmode.EventTypeEventsAPI:
					eventsAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
					if !ok {
						continue
					}

					if err := b.handleEventAPI(ctx, eventsAPI); err != nil {
						slog.ErrorContext(ctx, "error handling event", "error", err)
					}

					b.client.AckCtx(ctx, evt.Request.EnvelopeID, nil)
				}
			}
		}
	}()

	return b.client.RunContext(ctx)
}

func (b *integration) handleEventAPI(ctx context.Context, event slackevents.EventsAPIEvent) error {
	switch event.Type {
	case slackevents.CallbackEvent:
		switch ev := event.InnerEvent.Data.(type) {
		case *slackevents.MessageEvent:
			err := b.bot.Notify(ctx, ev)
			if err != nil {
				return fmt.Errorf("notifying update for channel: %w", err)
			}
		default:
			return fmt.Errorf("unhandled event: %T", ev)
		}
	default:
		return fmt.Errorf("unhandled event type: %s", event.Type)
	}

	return nil
}

func (b *integration) Client() *slack.Client {
	return &b.client.Client
}
