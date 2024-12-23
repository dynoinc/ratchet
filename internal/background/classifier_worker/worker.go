package classifier_worker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riverqueue/river"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
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
			return nil, err
		}
	}

	return &classifierWorker{
		incidentBinary: c.IncidentClassificationBinary,
		bot:            bot,
	}, nil
}

type action string
type priority string

const (
	actionNone          action = "none"
	actionOpenIncident  action = "open_incident"
	actionCloseIncident action = "close_incident"

	priorityHigh priority = "HIGH"
	priorityLow  priority = "LOW"
)

func (a *action) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	switch s {
	case "none":
		*a = actionNone
	case "open_incident":
		*a = actionOpenIncident
	case "close_incident":
		*a = actionCloseIncident
	default:
		return fmt.Errorf("unknown action: %s", s)
	}

	return nil
}

func (p *priority) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	switch s {
	case "HIGH":
		*p = priorityHigh
	case "LOW":
		*p = priorityLow
	default:
		return fmt.Errorf("unknown priority: %s", s)
	}

	return nil
}

type incidentAction struct {
	Action  action `json:"action"`
	Alert   string `json:"alert"`
	Service string `json:"service"`

	// Only used for open_incident.
	Priority priority `json:"priority,omitempty"`
}

func (w *classifierWorker) Work(ctx context.Context, job *river.Job[background.ClassifierArgs]) error {
	msg, err := w.bot.GetMessage(ctx, job.Args.ChannelID, job.Args.SlackTS)
	if err != nil {
		return err
	}

	if w.incidentBinary != "" {
		username := msg.Attrs.Message.Username
		text := msg.Attrs.Message.Text
		action, err := runIncidentBinary(w.incidentBinary, username, text)
		if err != nil {
			slog.ErrorContext(ctx, "failed to classify incident with binary", "error", err)
		}

		slog.InfoContext(ctx, "classified incident", "text", text, "action", action)
		if action.Action != actionNone {
			if err := processIncidentAction(ctx, w.bot, msg, action); err != nil {
				if errors.Is(err, internal.ErrNoOpenIncident) {
					// Ignore errors when closing incidents that are not open.
					slog.WarnContext(ctx, "failed to process incident action", "error", err)
					return nil
				}

				return fmt.Errorf("failed to process incident action: %w", err)
			}

			return nil
		}
	}

	subType := msg.Attrs.Message.SubType
	if subType == "bot_message" {
		botName := msg.Attrs.Message.Username
		return w.bot.TagAsBotNotification(ctx, msg.ChannelID, msg.SlackTs, botName)
	}

	userID := msg.Attrs.Message.User
	return w.bot.TagAsUserMessage(ctx, msg.ChannelID, msg.SlackTs, userID)
}

func processIncidentAction(
	ctx context.Context,
	bot *internal.Bot,
	msg schema.Message,
	action *incidentAction,
) error {
	t, err := slack_integration.TsToTime(msg.SlackTs)
	if err != nil {
		return fmt.Errorf("failed to parse Slack timestamp: %w", err)
	}

	tz := pgtype.Timestamptz{Time: t, Valid: true}

	switch action.Action {
	case actionOpenIncident:
		_, err := bot.OpenIncident(ctx, schema.OpenIncidentParams{
			ChannelID:      msg.ChannelID,
			SlackTs:        msg.SlackTs,
			Alert:          action.Alert,
			Service:        action.Service,
			Priority:       string(action.Priority),
			StartTimestamp: tz,
		})
		if err != nil {
			return fmt.Errorf("failed to open incident: %w", err)
		}
	case actionCloseIncident:
		if err := bot.CloseIncident(ctx, msg.ChannelID, msg.SlackTs, action.Alert, action.Service, tz); err != nil {
			return fmt.Errorf("failed to close incident: %w", err)
		}
	}

	return nil
}

type binaryInput struct {
	Username string `json:"username"`
	Text     string `json:"text"`
}

func runIncidentBinary(binaryPath string, username, text string) (*incidentAction, error) {
	input := binaryInput{
		Username: username,
		Text:     text,
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal input: %w", err)
	}

	var stdout bytes.Buffer
	cmd := exec.Command(binaryPath)
	cmd.Stdin = bytes.NewReader(inputJSON)
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("failed to run binary: %w", err)
	}

	var output incidentAction
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return nil, fmt.Errorf("failed to parse output: %w", err)
	}

	return &output, nil
}
