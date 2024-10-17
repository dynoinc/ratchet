package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

type slackBot struct {
	botUserID string

	client       *slack.Client
	socketClient *socketmode.Client
}

func SetupSlack(ctx context.Context, appToken, botToken string) error {
	client := slack.New(
		botToken,
		//slack.OptionDebug(true),
		slack.OptionAppLevelToken(appToken),
		slack.OptionLog(log.New(os.Stdout, "slack: ", log.Lshortfile|log.LstdFlags)),
	)

	authTest, err := client.AuthTest()
	if err != nil {
		return fmt.Errorf("Slack API test failed: %v", err)
	}
	log.Printf("Bot ID is %s", authTest.UserID)

	socketClient := socketmode.New(
		client,
		//socketmode.OptionDebug(true),
		socketmode.OptionLog(log.New(os.Stdout, "socketmode: ", log.Lshortfile|log.LstdFlags)),
	)

	slackBot := &slackBot{
		botUserID:    authTest.UserID,
		client:       client,
		socketClient: socketClient,
	}

	go slackBot.processSocketModeEvents(ctx)
	go slackBot.socketClient.RunContext(ctx)
	return nil
}

func (b *slackBot) processSocketModeEvents(ctx context.Context) {
	for evt := range b.socketClient.Events {
		if evt.Type == socketmode.EventTypeHello || evt.Type == socketmode.EventTypeConnecting || evt.Type == socketmode.EventTypeConnected {
			continue
		}

		log.Printf("Got socket mode event: %#v", evt)

		switch evt.Type {
		case socketmode.EventTypeEventsAPI:
			eventsAPIEvent, _ := evt.Data.(slackevents.EventsAPIEvent)
			log.Printf("Got a events API event: %v", eventsAPIEvent)

			switch eventsAPIEvent.InnerEvent.Type {
			case "member_joined_channel":
				memberEvent := eventsAPIEvent.InnerEvent.Data.(*slackevents.MemberJoinedChannelEvent)

				// Only trigger modal when the bot itself joins the channel
				if memberEvent.User == b.botUserID && memberEvent.Inviter != "" {
					handleBotJoin(ctx, b.socketClient, memberEvent)
				}
			}
		case socketmode.EventTypeInteractive:
			callback, ok := evt.Data.(slack.InteractionCallback)
			if ok {
				if callback.Type == slack.InteractionTypeViewSubmission {
					handleModalSubmission(b.socketClient, callback)
				}
			}
		}

		if evt.Request != nil {
			b.socketClient.Ack(*evt.Request)
		}
	}
}

func handleBotJoin(ctx context.Context, client *socketmode.Client, event *slackevents.MemberJoinedChannelEvent) {
	modalRequest := slack.ModalViewRequest{
		Type:   slack.VTModal,
		Title:  slack.NewTextBlockObject("plain_text", "Ratchet", true, false),
		Submit: slack.NewTextBlockObject("plain_text", "Submit", true, false),
		Close:  slack.NewTextBlockObject("plain_text", "Cancel", true, false),
		Blocks: slack.Blocks{
			BlockSet: []slack.Block{
				slack.NewInputBlock(
					"team_name_block",
					slack.NewTextBlockObject("plain_text", "Team name", false, false),
					nil,
					slack.NewPlainTextInputBlockElement(
						slack.NewTextBlockObject("plain_text", "Enter your team name", false, false),
						"plain_text_input-action",
					),
				),
				slack.NewInputBlock(
					"channel_type_block",
					slack.NewTextBlockObject("plain_text", "Channel type", false, false),
					nil,
					slack.NewRadioButtonsBlockElement(
						"radio_buttons-action",
						slack.NewOptionBlockObject("ops", slack.NewTextBlockObject("mrkdwn", "*Ops* - Alerts are routed here", false, false), nil),
						slack.NewOptionBlockObject("help", slack.NewTextBlockObject("mrkdwn", "*Help* - Users ask for help here", false, false), nil),
						slack.NewOptionBlockObject("bots", slack.NewTextBlockObject("mrkdwn", "*Bots* - Automation posts updates here", false, false), nil),
					),
				),
			},
		},
	}

	encoded, _ := json.Marshal(modalRequest)
	log.Println(string(encoded))
	_, err := client.Client.OpenViewContext(ctx, event.Inviter, modalRequest)
	if err != nil {
		log.Printf("Failed to open modal: %v", err)
	}
}

func handleModalSubmission(client *socketmode.Client, callback slack.InteractionCallback) {
	teamName := callback.View.State.Values["team_name_block"]["plain_text_input-action"].Value
	channelType := callback.View.State.Values["channel_type_block"]["radio_buttons-action"].SelectedOption.Value
	log.Printf("Team name: %s, Channel type: %s", teamName, channelType)

	// Exit the channel
	_, _, err := client.Client.PostMessage(callback.Channel.ID, slack.MsgOptionText("Exiting channel", false))
	if err != nil {
		log.Printf("Failed to post message: %v", err)
	}
	_, err = client.Client.LeaveConversation(callback.Channel.ID)
	if err != nil {
		log.Printf("Failed to leave channel: %v", err)
	}
}
