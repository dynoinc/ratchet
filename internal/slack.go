package internal

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/rajatgoel/ratchet/internal/schema"
)

type SlackBot struct {
	BotUserID string

	api    *slack.Client
	client *socketmode.Client

	dbQueries *schema.Queries
}

func NewSlackBot(ctx context.Context, appToken, botToken string, dbQueries *schema.Queries) (*SlackBot, error) {
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

	return &SlackBot{
		BotUserID: authTest.UserID,
		api:       api,
		client:    socketClient,
		dbQueries: dbQueries,
	}, nil
}

func (b *SlackBot) Run(ctx context.Context) error {
	go func() {
		commands := map[string]func(ctx context.Context, evt socketmode.Event, cmd slack.SlashCommand){
			"track-channel": b.handleTrackChannel,
		}

		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-b.client.Events:
				switch evt.Type {
				case socketmode.EventTypeSlashCommand:
					cmd, ok := evt.Data.(slack.SlashCommand)
					if !ok {
						continue
					}

					subCmd := ""
					if args := strings.Fields(cmd.Text); len(args) > 0 {
						subCmd = args[0]
					}
					if handler, ok := commands[subCmd]; ok {
						handler(ctx, evt, cmd)
						continue
					}
					b.client.Ack(*evt.Request, &slack.Msg{
						Text: fmt.Sprintf("Please provide a command: %v", slices.Collect(maps.Keys(commands))),
					})
				case socketmode.EventTypeEventsAPI:
					eventsAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
					if !ok {
						continue
					}
					b.client.Ack(*evt.Request)
					b.handleEventAPI(eventsAPI)
				}
			}
		}
	}()

	return b.client.RunContext(ctx)
}

func (b *SlackBot) handleTrackChannel(ctx context.Context, evt socketmode.Event, cmd slack.SlashCommand) {
	args := strings.Fields(cmd.Text)
	if len(args) < 2 || args[1] == "" {
		b.client.Ack(*evt.Request, &slack.Msg{
			Text: "Please provide a valid team name: `/ratchet track-channel <team-name>`",
		})
		return
	}

	// Join the channel
	teamName := args[1]
	if _, _, _, err := b.api.JoinConversationContext(ctx, cmd.ChannelID); err != nil {
		b.client.Ack(*evt.Request, &slack.Msg{
			Text: fmt.Sprintf("Failed to join channel %s: %v", cmd.ChannelID, err),
		})
		return
	}

	// Insert the team name into the database
	channel, err := b.dbQueries.InsertSlackChannel(ctx, schema.InsertSlackChannelParams{
		ChannelID: cmd.ChannelID,
		TeamName:  teamName,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "slack_channels_pkey" {
			b.client.Ack(*evt.Request, &slack.Msg{
				Text: fmt.Sprintf("Channel %s is already being tracked under %s", cmd.ChannelID, teamName),
			})
		} else {
			b.client.Ack(*evt.Request, &slack.Msg{
				Text: fmt.Sprintf("Failed to track channel %s under %s: %v", cmd.ChannelID, teamName, err),
			})
		}
	}

	b.client.Ack(*evt.Request, &slack.Msg{
		Text: fmt.Sprintf("Successfully joined channel %s and started tracking messages under %s", channel.ChannelID, teamName),
	})
}

func (b *SlackBot) handleEventAPI(event slackevents.EventsAPIEvent) {
	switch event.Type {
	case slackevents.CallbackEvent:
		switch ev := event.InnerEvent.Data.(type) {
		case *slackevents.MessageEvent:
			// Process the message here
			log.Printf("Channel: %s, User: %s, Message: %s",
				ev.Channel, ev.User, ev.Text)

			// Add your message processing logic here
		}
	}
}
