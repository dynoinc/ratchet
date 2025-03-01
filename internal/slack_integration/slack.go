package slack_integration

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/dynoinc/ratchet/internal"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

type Config struct {
	BotToken   string `split_words:"true" required:"true"`
	AppToken   string `split_words:"true" required:"true"`
	DevChannel string `split_words:"true" default:"ratchet-test"`
}

type Integration interface {
	Run(ctx context.Context) error
	GetBotChannels() ([]slack.Channel, error)
	PostMessage(ctx context.Context, channelID string, messageBlocks ...slack.Block) error
	PostThreadReply(ctx context.Context, channelID, ts string, messageBlocks ...slack.Block) error
	Client() *slack.Client
	GetConversationInfo(ctx context.Context, channelID string) (*slack.Channel, error)
	GetConversationHistory(ctx context.Context, channelID string, lastNMsgs int) ([]slack.Message, error)
	GetConversationReplies(ctx context.Context, channelID, ts string) ([]slack.Message, error)
	BotUserID() string
	GetUserIDByEmail(ctx context.Context, email string) (string, error)
	GetFiles(ctx context.Context, channelID, ts string) ([]slack.File, error)
	FetchFile(ctx context.Context, file slack.File) ([]byte, error)
	UploadFileToThread(ctx context.Context, channelID, threadTs, filename string, content []byte, title string) error
}

type integration struct {
	c Config

	botUserID string
	client    *socketmode.Client

	bot *internal.Bot
}

func New(ctx context.Context, c Config, bot *internal.Bot) (Integration, error) {
	api := slack.New(c.BotToken, slack.OptionAppLevelToken(c.AppToken))

	authTest, err := api.AuthTestContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("slack API test failed: %w", err)
	}

	socketClient := socketmode.New(api)

	return &integration{
		c:         c,
		botUserID: authTest.UserID,
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
						slog.ErrorContext(ctx, "handling event", "error", err)
					}

					if err := b.client.AckCtx(ctx, evt.Request.EnvelopeID, nil); err != nil {
						slog.ErrorContext(ctx, "acknowledging event",
							"error", err,
							"envelope_id", evt.Request.EnvelopeID,
						)
					}
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
			if err := b.bot.NotifyMessage(ctx, ev); err != nil {
				return fmt.Errorf("notifying message for channel: %w", err)
			}
		case *slackevents.ReactionAddedEvent:
			if err := b.bot.NotifyReactionAdded(ctx, ev); err != nil {
				return fmt.Errorf("notifying reaction added: %w", err)
			}
		case *slackevents.ReactionRemovedEvent:
			if err := b.bot.NotifyReactionRemoved(ctx, ev); err != nil {
				return fmt.Errorf("notifying reaction removed: %w", err)
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

func (b *integration) GetConversationInfo(ctx context.Context, channelID string) (*slack.Channel, error) {
	return b.client.GetConversationInfoContext(ctx, &slack.GetConversationInfoInput{
		ChannelID: channelID,
	})
}

func (b *integration) GetConversationHistory(ctx context.Context, channelID string, lastNMsgs int) ([]slack.Message, error) {
	params := &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Latest:    internal.TimeToTs(time.Now()),
		Limit:     lastNMsgs,
	}
	var messages []slack.Message
	for {
		history, err := b.client.GetConversationHistoryContext(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("getting conversation history for channel ID %s: %w", channelID, err)
		}

		messages = append(messages, history.Messages...)
		if !history.HasMore || len(messages) >= lastNMsgs {
			break
		}

		params.Cursor = history.ResponseMetadata.Cursor
		params.Latest = history.Messages[len(history.Messages)-1].Timestamp
	}

	return messages, nil
}

func (b *integration) GetConversationReplies(ctx context.Context, channelID, ts string) ([]slack.Message, error) {
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

func (b *integration) GetBotChannels() ([]slack.Channel, error) {
	params := &slack.GetConversationsForUserParameters{
		UserID:          b.botUserID,
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

func (b *integration) PostMessage(ctx context.Context, channelID string, messageBlocks ...slack.Block) error {
	if b.c.DevChannel != "" {
		channelID = b.c.DevChannel
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

func (b *integration) PostThreadReply(ctx context.Context, channelID, ts string, messageBlocks ...slack.Block) error {
	msgOptions := []slack.MsgOption{slack.MsgOptionBlocks(messageBlocks...)}
	if b.c.DevChannel != "" {
		channelID = b.c.DevChannel
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

func (b *integration) BotUserID() string {
	return b.botUserID
}

func (b *integration) GetUserIDByEmail(ctx context.Context, email string) (string, error) {
	user, err := b.client.GetUserByEmail(email)
	if err != nil {
		return "", fmt.Errorf("getting user by email %s: %w", email, err)
	}
	return user.ID, nil
}

// GetFiles returns the files from the given message in the channel, use until we decide to store the file in the database
func (b *integration) GetFiles(ctx context.Context, channelID, ts string) ([]slack.File, error) {
	history, err := b.client.GetConversationHistoryContext(ctx, &slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Latest:    ts,
		Limit:     1,
	})
	if err != nil {
		return nil, fmt.Errorf("getting files for channel ID %s: %w", channelID, err)
	}
	if len(history.Messages) == 0 {
		return nil, fmt.Errorf("no messages found for channel ID %s: %w", channelID, err)
	}
	return history.Messages[0].Files, nil
}

func (b *integration) FetchFile(ctx context.Context, file slack.File) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, file.URLPrivateDownload, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Add("Authorization", "Bearer "+b.c.BotToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading file: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (b *integration) UploadFileToThread(ctx context.Context, channelID, threadTs, filename string, content []byte, title string) error {
	if b.c.DevChannel != "" {
		channelID = b.c.DevChannel
	}
	params := slack.UploadFileV2Parameters{
		Channel:         channelID,
		ThreadTimestamp: threadTs,
		Filename:        filename,
		Title:           title,
		Content:         string(content),
		FileSize:        int(len(content)),
	}
	_, err := b.client.UploadFileV2Context(ctx, params)
	if err != nil {
		return fmt.Errorf("uploading file to thread: %w", err)
	}
	return nil
}
