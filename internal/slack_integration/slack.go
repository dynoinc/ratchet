package slack_integration

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/dynoinc/ratchet/internal"
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

func TsToTime(ts string) (time.Time, error) {
	// Split the timestamp into seconds and microseconds
	parts := strings.Split(ts, ".")
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("invalid Slack timestamp format: %s", ts)
	}

	// Convert seconds and microseconds to integers
	seconds, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse seconds: %w", err)
	}

	microseconds, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse microseconds: %w", err)
	}

	// Create a time.Time object using Unix seconds and nanoseconds
	return time.Unix(seconds, microseconds*1000).UTC(), nil
}

func TimeToTs(t time.Time) string {
	// Convert time.Time to Unix seconds and nanoseconds
	seconds := t.Unix()
	nanoseconds := int64(t.Nanosecond())

	// Convert Unix seconds and nanoseconds to a Slack timestamp
	return fmt.Sprintf("%d.%06d", seconds, nanoseconds/1000)
}
