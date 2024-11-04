package slack

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
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
		case *slackevents.MemberLeftChannelEvent:
			if ev.User != b.BotUserID {
				return
			}

			if err := b.bot.DisableChannel(ctx, ev.Channel); err != nil {
				log.Printf("Error disabling channel: %v", err)
			}
		case *slackevents.MemberJoinedChannelEvent:
			if ev.User != b.BotUserID {
				return
			}

			if err := b.bot.InsertOrEnableChannel(ctx, ev.Channel); err != nil {
				log.Printf("Error enabling channel: %v", err)
			}
		case *slackevents.MessageEvent:
			if ev.ThreadTimeStamp != "" {
				return
			}

			err := b.bot.AddMessage(ctx, ev.Channel, ev.TimeStamp, dto.MessageAttrs{Upstream: *ev})
			if err != nil {
				log.Printf("Error adding message: %v", err)
				return
			}
		default:
			log.Printf("Unhandled event: %v", ev)
		}
	}
}
