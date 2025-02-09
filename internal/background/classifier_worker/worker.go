package classifier_worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime/debug"

	"github.com/jackc/pgx/v5"
	"github.com/pgvector/pgvector-go"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type Config struct {
	IncidentClassificationBinary string `split_words:"true" required:"true"`
}

type classifierWorker struct {
	river.WorkerDefaults[background.ClassifierArgs]

	incidentBinary string
	bot            *internal.Bot
	llmClient      *llm.Client
}

func New(c Config, bot *internal.Bot, llmClient *llm.Client) (river.Worker[background.ClassifierArgs], error) {
	if _, err := exec.LookPath(c.IncidentClassificationBinary); err != nil {
		return nil, fmt.Errorf("looking up incident classification binary: %w", err)
	}

	return &classifierWorker{
		incidentBinary: c.IncidentClassificationBinary,
		bot:            bot,
		llmClient:      llmClient,
	}, nil
}

func (w *classifierWorker) Work(ctx context.Context, job *river.Job[background.ClassifierArgs]) error {
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("stacktrace from panic: %+v\n", string(debug.Stack()))
			panic(r)
		}
	}()

	msg, err := w.bot.GetMessage(ctx, job.Args.ChannelID, job.Args.SlackTS)
	if err != nil {
		if errors.Is(err, internal.ErrMessageNotFound) {
			slog.WarnContext(ctx, "message not found", "channel_id", job.Args.ChannelID, "slack_ts", job.Args.SlackTS)
			return nil
		}

		return fmt.Errorf("getting message: %w", err)
	}

	action, err := runIncidentBinary(w.incidentBinary, msg.Attrs.Message.BotUsername, msg.Attrs.Message.Text)
	if err != nil {
		return fmt.Errorf("classifying incident with binary: %w", err)
	}

	embedding, err := w.llmClient.GenerateEmbedding(ctx, msg.Attrs.Message.Text)
	if err != nil {
		return fmt.Errorf("generating embedding: %w", err)
	}

	vector := pgvector.NewVector(embedding)
	params := schema.UpdateMessageAttrsParams{
		ChannelID: job.Args.ChannelID,
		Ts:        job.Args.SlackTS,
		Embedding: &vector,
	}
	if action.Action != dto.ActionNone {
		params.Attrs = dto.MessageAttrs{IncidentAction: action}
	}

	slog.DebugContext(ctx, "classified message", "channel_id", params.ChannelID, "slack_ts", params.Ts, "text", msg.Attrs.Message.Text, "attrs", params.Attrs)

	tx, err := w.bot.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	qtx := schema.New(w.bot.DB).WithTx(tx)
	if err := qtx.UpdateMessageAttrs(ctx, params); err != nil {
		return fmt.Errorf("updating message %s (ts=%s): %w", params.ChannelID, params.Ts, err)
	}

	if params.Attrs.IncidentAction.Action == dto.ActionOpenIncident && !job.Args.IsBackfill {
		riverclient := river.ClientFromContext[pgx.Tx](ctx)
		if _, err := riverclient.InsertTx(ctx, tx, background.PostRunbookWorkerArgs{
			ChannelID: params.ChannelID,
			SlackTS:   params.Ts,
		}, nil); err != nil {
			return fmt.Errorf("scheduling runbook worker: %w", err)
		}
	}

	if _, err = river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
		return fmt.Errorf("completing job: %w", err)
	}

	return tx.Commit(ctx)
}

type binaryInput struct {
	Username string `json:"username"`
	Text     string `json:"text"`
}

func runIncidentBinary(binaryPath string, username, text string) (dto.IncidentAction, error) {
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
