package slack_integration

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dynoinc/ratchet/internal"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

type Config struct {
	BotToken     string `split_words:"true" required:"true"`
	AppToken     string `split_words:"true" required:"true"`
	DevChannelID string `split_words:"true" default:"ratchet-test"`
}

type Integration struct {
	c Config

	BotUserID string
	client    *socketmode.Client

	bot *internal.Bot
}

func New(ctx context.Context, c Config, bot *internal.Bot) (*Integration, error) {
	api := slack.New(c.BotToken, slack.OptionAppLevelToken(c.AppToken))

	authTest, err := api.AuthTestContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("slack API test failed: %w", err)
	}

	socketClient := socketmode.New(api)

	return &Integration{
		c:         c,
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

func (b *Integration) handleEventAPI(ctx context.Context, event slackevents.EventsAPIEvent) error {
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

func (b *Integration) Client() *slack.Client {
	return &b.client.Client
}

func (b *Integration) GetConversationInfo(ctx context.Context, channelID string) (*slack.Channel, error) {
	return b.client.GetConversationInfoContext(ctx, &slack.GetConversationInfoInput{
		ChannelID: channelID,
	})
}

func (b *Integration) GetConversationHistory(ctx context.Context, channelID string) ([]slack.Message, error) {
	params := &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Latest:    internal.TimeToTs(time.Now()),
		Limit:     1000,
	}
	var messages []slack.Message
	for {
		history, err := b.client.GetConversationHistoryContext(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("getting conversation history for channel ID %s: %w", channelID, err)
		}

		messages = append(messages, history.Messages...)
		if !history.HasMore || len(messages) >= 10 {
			break
		}

		params.Cursor = history.ResponseMetadata.Cursor
		params.Latest = history.Messages[len(history.Messages)-1].Timestamp
	}

	return messages, nil
}

func (b *Integration) GetConversationReplies(ctx context.Context, channelID, ts string) ([]slack.Message, error) {
	params := &slack.GetConversationRepliesParameters{
		ChannelID: channelID,
		Timestamp: ts,
	}

	var messages []slack.Message
	for {
		threadMessages, hasMore, nextCursor, err := b.client.GetConversationRepliesContext(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("getting conversation replies for channel ID %s: %w", channelID, err)
		}

		messages = append(messages, threadMessages...)
		if !hasMore {
			break
		}

		params.Cursor = nextCursor
	}

	return messages, nil
}

func (b *Integration) GetBotChannels() ([]slack.Channel, error) {
	params := &slack.GetConversationsForUserParameters{
		UserID:          b.BotUserID,
		Types:           []string{"public_channel"},
		ExcludeArchived: true,
	}

	channels := []slack.Channel{}
	for {
		response, nextCursor, err := b.client.GetConversationsForUserContext(context.Background(), params)
		if err != nil {
			return nil, err
		}

		channels = append(channels, response...)

		if nextCursor == "" {
			break
		}

		params.Cursor = nextCursor
	}

	return channels, nil
}

func (b *Integration) PostMessage(ctx context.Context, channelID string, messageBlocks ...slack.Block) error {
	if b.c.DevChannelID != "" {
		channelID = b.c.DevChannelID
	}

	_, _, err := b.client.PostMessage(
		channelID,
		slack.MsgOptionBlocks(messageBlocks...),
	)
	if err != nil {
		return fmt.Errorf("posting report message: %w", err)
	}

	return nil
}

func (b *Integration) PostThreadReply(ctx context.Context, channelID, ts string, messageBlocks ...slack.Block) error {
	msgOptions := []slack.MsgOption{slack.MsgOptionBlocks(messageBlocks...)}
	if b.c.DevChannelID != "" {
		channelID = b.c.DevChannelID
	} else {
		msgOptions = append(msgOptions, slack.MsgOptionTS(ts))
	}

	if _, _, err := b.client.PostMessage(
		channelID,
		msgOptions...); err != nil {
		return fmt.Errorf("posting thread reply: %w", err)
	}

	return nil
}
