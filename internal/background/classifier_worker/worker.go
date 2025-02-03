package classifier_worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type Config struct {
	IncidentClassificationBinary string `split_words:"true"`
}

type classifierWorker struct {
	river.WorkerDefaults[background.ClassifierArgs]

	incidentBinary string
	bot            *internal.Bot
}

func New(c Config, bot *internal.Bot) (river.Worker[background.ClassifierArgs], error) {
	if c.IncidentClassificationBinary != "" {
		if _, err := exec.LookPath(c.IncidentClassificationBinary); err != nil {
			return nil, fmt.Errorf("looking up incident classification binary: %w", err)
		}
	}

	return &classifierWorker{
		incidentBinary: c.IncidentClassificationBinary,
		bot:            bot,
	}, nil
}

func (w *classifierWorker) Work(ctx context.Context, job *river.Job[background.ClassifierArgs]) error {
	msg, err := w.bot.GetMessage(ctx, job.Args.ChannelID, job.Args.SlackTS)
	if err != nil {
		if errors.Is(err, internal.ErrMessageNotFound) {
			slog.WarnContext(ctx, "message not found", "channel_id", job.Args.ChannelID, "slack_ts", job.Args.SlackTS)
			return nil
		}

		return fmt.Errorf("getting message: %w", err)
	}

	action, err := runIncidentBinary(w.incidentBinary, msg.Message.BotUsername, msg.Message.Text)
	if err != nil {
		return fmt.Errorf("classifying incident with binary: %w", err)
	}

	slog.InfoContext(
		ctx, "classified incident",
		"text", msg.Message.Text,
		"channel_id", job.Args.ChannelID,
		"slack_ts", job.Args.SlackTS,
		"action", action,
	)
	if action.Action != dto.ActionNone {
		tx, err := w.bot.DB.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)

		qtx := schema.New(w.bot.DB).WithTx(tx)
		if err := qtx.UpdateMessageAttrs(ctx, schema.UpdateMessageAttrsParams{
			ChannelID: job.Args.ChannelID,
			Ts:        job.Args.SlackTS,
			Attrs:     dto.MessageAttrs{IncidentAction: action},
		}); err != nil {
			return fmt.Errorf("updating message attrs: %w", err)
		}

		if _, err = river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
			return fmt.Errorf("completing job: %w", err)
		}

		return tx.Commit(ctx)
	}

	return nil
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
		return dto.IncidentAction{}, fmt.Errorf("failed to marshal input: %w", err)
	}

	var stdout bytes.Buffer
	cmd := exec.Command(binaryPath)
	cmd.Stdin = bytes.NewReader(inputJSON)
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return dto.IncidentAction{}, fmt.Errorf("failed to run binary %s: %w", binaryPath, err)
	}

	var output dto.IncidentAction
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return dto.IncidentAction{}, fmt.Errorf("failed to parse output from binary: %w", err)
	}

	return output, nil
}
