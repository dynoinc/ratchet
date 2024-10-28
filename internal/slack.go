package internal

import (
	"context"
	"fmt"
	"log"
	"os"

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
	channels, _, err := b.api.GetConversationsContext(ctx, &slack.GetConversationsParameters{})
	if err != nil {
		return fmt.Errorf("error getting conversations: %v", err)
	}

	channelIDs := make([]string, 0, len(channels))
	for _, channel := range channels {
		channelIDs = append(channelIDs, channel.ID)
	}

	existingChannels, err := b.dbQueries.GetSlackChannelsByIDs(ctx, channelIDs)
	if err != nil {
		return fmt.Errorf("error getting channels from db: %v", err)
	}

	existingChannelMap := make(map[string]struct{})
	for _, channel := range existingChannels {
		existingChannelMap[channel.ChannelID] = struct{}{}
	}

	for _, channel := range channels {
		if _, exists := existingChannelMap[channel.ID]; !exists {
			if _, err := b.api.LeaveConversationContext(ctx, channel.ID); err != nil {
				log.Printf("Error leaving conversation %s: %v", channel.ID, err)
			} else {
				log.Printf("Left conversation %s", channel.ID)
			}
		}
	}

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
					b.handleEventAPI(eventsAPI)
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

func (b *SlackBot) handleOnboardCallback(ctx context.Context, interaction slack.InteractionCallback) {
	// Check if channel already exists and is enabled. If yes, do nothing.
	channel, err := b.dbQueries.GetSlackChannelByID(ctx, interaction.Channel.ID)
	if err == nil && channel.Enabled {
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

func (b *SlackBot) handleOnboardModalSubmit(ctx context.Context, interaction slack.InteractionCallback) {
	teamName := interaction.View.State.Values["team_name_block"]["team_name_input"].Value
	channelID := interaction.View.PrivateMetadata
	log.Printf("Team name: %s, channelID: %s", teamName, channelID)
	existingChannel, err := b.dbQueries.GetSlackChannelByID(ctx, channelID)
	if err == nil {
		if existingChannel.Enabled {
			if _, _, err := b.api.PostMessageContext(
				ctx,
				interaction.User.ID,
				slack.MsgOptionText(fmt.Sprintf("Channel %s is already registered under team %s", channelID, existingChannel.TeamName), false),
			); err != nil {
				log.Printf("Error posting message: %v", err)
			}

			return
		}

		if _, err := b.dbQueries.UpdateSlackChannel(ctx, schema.UpdateSlackChannelParams{
			ChannelID: channelID,
			TeamName:  teamName,
		}); err != nil {
			if _, _, err := b.api.PostMessageContext(
				ctx,
				interaction.User.ID,
				slack.MsgOptionText(fmt.Sprintf("Failed to update channel %s: %v", channelID, err), false),
			); err != nil {
				log.Printf("Error posting message: %v", err)
			}
			return
		}
	} else {
		if _, err := b.dbQueries.InsertSlackChannel(ctx, schema.InsertSlackChannelParams{
			ChannelID: channelID,
			TeamName:  teamName,
		}); err != nil {
			if _, _, err := b.api.PostMessageContext(
				ctx,
				interaction.User.ID,
				slack.MsgOptionText(fmt.Sprintf("Failed to register channel %s: %v", channelID, err), false),
			); err != nil {
				log.Printf("Error posting message: %v", err)
			}
			return
		}
	}

	if _, _, err := b.api.PostMessageContext(ctx, channelID, slack.MsgOptionText(
		fmt.Sprintf("Successfully registered channel %s under team %s", channelID, teamName),
		false)); err != nil {
		log.Printf("Error posting message: %v", err)
	}
}

func (b *SlackBot) handleEventAPI(event slackevents.EventsAPIEvent) {
	switch event.Type {
	case slackevents.CallbackEvent:
		switch ev := event.InnerEvent.Data.(type) {
		case *slackevents.MemberLeftChannelEvent:
			if ev.User != b.BotUserID {
				return
			}

			if _, err := b.dbQueries.DisableSlackChannel(context.Background(), ev.Channel); err != nil {
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

			b.api.PostMessage(ev.Channel, slack.MsgOptionAttachments(attachment))
		case *slackevents.MessageEvent:
			// Process the message here
			log.Printf("Channel: %s, User: %s, Message: %s",
				ev.Channel, ev.User, ev.Text)

			// Add your message processing logic here
		}
	}
}

func (b *SlackBot) handleInteraction(ctx context.Context, interaction slack.InteractionCallback) {
	if interaction.CallbackID == "onboard_callback" {
		b.handleOnboardCallback(ctx, interaction)
		return
	}
	if interaction.View.CallbackID == "onboard_modal_callback" {
		b.handleOnboardModalSubmit(ctx, interaction)
		return
	}
}
