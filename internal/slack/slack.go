package slack

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/rajatgoel/ratchet/internal"
)

type SlackIntegration struct {
	BotUserID string

	api    *slack.Client
	client *socketmode.Client
	bot    *internal.Bot
}

func New(ctx context.Context, appToken, botToken string, bot *internal.Bot) (*SlackIntegration, error) {
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

	return &SlackIntegration{
		BotUserID: authTest.UserID,
		api:       api,
		client:    socketClient,
		bot:       bot,
	}, nil
}

func (b *SlackIntegration) Run(ctx context.Context) error {
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
				case socketmode.EventTypeInteractive:
					interaction, ok := evt.Data.(slack.InteractionCallback)
					if !ok {
						continue
					}
					b.client.Ack(*evt.Request)
					b.handleInteraction(ctx, interaction)
				}
			}
		}
	}()

	return b.client.RunContext(ctx)
}

func (b *SlackIntegration) handleOnboardCallback(ctx context.Context, interaction slack.InteractionCallback) {
	// Insert an intent to onboard the channel.
	enabled, err := b.bot.InsertIntent(ctx, interaction.Channel.ID)
	if err != nil {
		log.Printf("Error inserting intent: %v", err)
		return
	}
	if enabled {
		return
	}

	modal := slack.ModalViewRequest{
		Type:            slack.VTModal,
		Title:           slack.NewTextBlockObject("plain_text", "Ratchet onboarding", false, false),
		Submit:          slack.NewTextBlockObject("plain_text", "Submit", false, false),
		Close:           slack.NewTextBlockObject("plain_text", "Cancel", false, false),
		CallbackID:      "onboard_modal_callback",
		PrivateMetadata: interaction.Channel.ID,
		Blocks: slack.Blocks{
			BlockSet: []slack.Block{
				slack.InputBlock{
					Type:    slack.MBTInput,
					BlockID: "team_name_block",
					Label:   slack.NewTextBlockObject("plain_text", "Enter team name", false, false),
					Element: slack.PlainTextInputBlockElement{
						Type:     slack.METPlainTextInput,
						ActionID: "team_name_input",
					},
				},
			},
		},
	}

	_, err = b.api.OpenViewContext(ctx, interaction.TriggerID, modal)
	if err != nil {
		log.Printf("Error opening modal: %v", err)
	}
}

func (b *SlackIntegration) handleOnboardModalSubmit(ctx context.Context, interaction slack.InteractionCallback) {
	teamName := interaction.View.State.Values["team_name_block"]["team_name_input"].Value
	channelID := interaction.View.PrivateMetadata

	if err := b.bot.OnboardChannel(ctx, channelID, teamName); err != nil {
		if _, _, err := b.api.PostMessageContext(
			ctx,
			interaction.User.ID,
			slack.MsgOptionText(fmt.Sprintf("Error onboarding channel %v for team %v: %v", channelID, teamName, err), false),
		); err != nil {
			log.Printf("Error posting message: %v", err)
		}
		return
	}

	if _, _, err := b.api.PostMessageContext(ctx, channelID, slack.MsgOptionText(
		fmt.Sprintf("Successfully registered channel %s under team %s", channelID, teamName),
		false)); err != nil {
		log.Printf("Error posting message: %v", err)
	}
}

func (b *SlackIntegration) handleEventAPI(ctx context.Context, event slackevents.EventsAPIEvent) {
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

			attachment := slack.Attachment{
				Text:       "Thanks for inviting ratchet to your channel. Click the button below to onboard.",
				CallbackID: "onboard_callback",
				Actions: []slack.AttachmentAction{
					{
						Name:  "onboard",
						Text:  "Click here to onboard",
						Type:  "button",
						Value: "onboard",
					},
				},
			}

			if _, _, err := b.api.PostMessageContext(ctx, ev.Channel, slack.MsgOptionAttachments(attachment)); err != nil {
				log.Printf("Error posting message: %v", err)
			}
		case *slackevents.MessageEvent:
			// Process the message here
			log.Printf("Channel: %s, User: %s, Message: %s",
				ev.Channel, ev.User, ev.Text)

			// Add your message processing logic here
		}
	}
}

func (b *SlackIntegration) handleInteraction(ctx context.Context, interaction slack.InteractionCallback) {
	if interaction.CallbackID == "onboard_callback" {
		b.handleOnboardCallback(ctx, interaction)
		return
	}
	if interaction.View.CallbackID == "onboard_modal_callback" {
		b.handleOnboardModalSubmit(ctx, interaction)
		return
	}
}
