package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/pgvector/pgvector-go"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/modules"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type Config struct {
	IncidentClassificationBinary string `split_words:"true" required:"true"`
}

type classifier struct {
	incidentBinary string
	bot            *internal.Bot
	llmClient      llm.Client
}

func New(c Config, bot *internal.Bot, llmClient llm.Client) (modules.Handler, error) {
	if _, err := exec.LookPath(c.IncidentClassificationBinary); err != nil {
		return nil, fmt.Errorf("looking up incident classification binary: %w", err)
	}

	return &classifier{
		incidentBinary: c.IncidentClassificationBinary,
		bot:            bot,
		llmClient:      llmClient,
	}, nil
}

func (w *classifier) Name() string {
	return "classifier"
}

func (w *classifier) EnabledForBackfill() bool {
	return true
}

func (w *classifier) OnMessage(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error {
	action, err := RunIncidentBinary(w.incidentBinary, msg.Message.BotUsername, msg.Message.Text)
	if err != nil {
		return fmt.Errorf("classifying incident with binary: %w", err)
	}

	embedding, err := w.llmClient.GenerateEmbedding(ctx, "search_document", msg.Message.Text)
	if err != nil {
		return fmt.Errorf("generating embedding: %w", err)
	}

	vector := pgvector.NewVector(embedding)
	params := schema.UpdateMessageAttrsParams{
		ChannelID: channelID,
		Ts:        slackTS,
		Embedding: &vector,
	}
	if action.Action != dto.ActionNone {
		params.Attrs = dto.MessageAttrs{IncidentAction: action}
	}

	slog.DebugContext(ctx, "classified message", "channel_id", params.ChannelID, "slack_ts", params.Ts, "text", msg.Message.Text, "attrs", params.Attrs)

	if err := schema.New(w.bot.DB).UpdateMessageAttrs(ctx, params); err != nil {
		return fmt.Errorf("updating message %s (ts=%s): %w", params.ChannelID, params.Ts, err)
	}

	return nil
}

type binaryInput struct {
	Username string `json:"username"`
	Text     string `json:"text"`
}

func RunIncidentBinary(binaryPath string, username, text string) (dto.IncidentAction, error) {
	input := binaryInput{
		Username: username,
		Text:     text,
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return dto.IncidentAction{}, fmt.Errorf("marshaling input: %w", err)
	}

	var stdout bytes.Buffer
	cmd := exec.Command(binaryPath)
	cmd.Stdin = bytes.NewReader(inputJSON)
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return dto.IncidentAction{}, fmt.Errorf("running incident classification binary %s: %w", binaryPath, err)
	}

	var output dto.IncidentAction
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return dto.IncidentAction{}, fmt.Errorf("parsing output from binary: %w", err)
	}

	return output, nil
}
