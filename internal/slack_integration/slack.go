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
		return nil, fmt.Errorf("slack API test failed: %v", err)
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

					b.client.Ack(*evt.Request)
					b.handleEventAPI(ctx, eventsAPI)
				}
			}
		}
	}()

	return b.client.RunContext(ctx)
}

func (b *integration) handleEventAPI(ctx context.Context, event slackevents.EventsAPIEvent) {
	switch event.Type {
	case slackevents.CallbackEvent:
		switch ev := event.InnerEvent.Data.(type) {
		case *slackevents.MessageEvent:
			if ev.ThreadTimeStamp == "" {
				err := b.bot.Notify(ctx, ev.Channel)
				if err != nil {
					slog.ErrorContext(ctx, "error notifying update for channel", "error", err, "channel_id", ev.Channel)
				}
			}
		default:
			slog.ErrorContext(ctx, "Unhandled event", "event", ev)
		}
	}
}

func (b *integration) SlackClient() *slack.Client {
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
		return time.Time{}, fmt.Errorf("failed to parse seconds: %v", err)
	}

	microseconds, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse microseconds: %v", err)
	}

	// Create a time.Time object using Unix seconds and nanoseconds
	return time.Unix(seconds, microseconds*1000).UTC(), nil
}
