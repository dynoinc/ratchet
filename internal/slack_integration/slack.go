package slack_integration

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

type Integration struct {
	BotUserID string
	client    *socketmode.Client

	bot *internal.Bot
}

func New(ctx context.Context, appToken, botToken string, bot *internal.Bot) (*Integration, error) {
	api := slack.New(
		botToken,
		slack.OptionAppLevelToken(appToken),
		slack.OptionLog(log.New(os.Stdout, "slack: ", log.Lshortfile|log.LstdFlags)),
	)

	authTest, err := api.AuthTestContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("slack API test failed: %v", err)
	}

	socketClient := socketmode.New(
		api,
		socketmode.OptionLog(log.New(os.Stdout, "socketmode: ", log.Lshortfile|log.LstdFlags)),
	)

	return &Integration{
		BotUserID: authTest.UserID,
		client:    socketClient,
		bot:       bot,
	}, nil
}

func (b *Integration) Run(ctx context.Context) error {
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

func (b *Integration) handleEventAPI(ctx context.Context, event slackevents.EventsAPIEvent) {
	switch event.Type {
	case slackevents.CallbackEvent:
		switch ev := event.InnerEvent.Data.(type) {
		case *slackevents.MessageEvent:
			if ev.ThreadTimeStamp == "" {
				// First, notify immediately with just the channel ID
				err := b.bot.Notify(ctx, ev.Channel)
				if err != nil {
					log.Printf("Error notifying: %v", err)
				}

				// Then, trigger background channel info fetch if needed
				go b.ensureChannelInfo(ctx, ev.Channel)
			}
		default:
			log.Printf("Unhandled event: %v", ev)
		}
	}
}

func (b *Integration) ensureChannelInfo(ctx context.Context, channelID string) {
	channel, err := schema.New(b.bot.DB).GetChannel(ctx, channelID)
	if err != nil {
		log.Printf("Error checking channel info: %v", err)
		return
	}

	if channel.ChannelName.String == "" {
		channelInfo, err := b.client.Client.GetConversationInfo(&slack.GetConversationInfoInput{
			ChannelID: channelID,
		})
		if err != nil {
			log.Printf("Error getting channel info: %v", err)
			return
		}

		_, err = schema.New(b.bot.DB).AddChannel(ctx, schema.AddChannelParams{
			ChannelID: channelID,
			ChannelName: pgtype.Text{
				String: channelInfo.Name,
				Valid:  true,
			},
		})
		if err != nil {
			log.Printf("Error storing channel info: %v", err)
		}
	}
}

func (b *Integration) SlackClient() *slack.Client {
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