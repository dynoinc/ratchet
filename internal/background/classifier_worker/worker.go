package classifier_worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/riverqueue/river"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/slack"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type Config struct {
	OpenAIAPIKey string `envconfig:"OPENAI_API_KEY" default:"fake-classifier-key"`
	OpenAIURL    string `envconfig:"OPENAI_URL" default:"http://localhost:11434/v1/"`
	OpenAIModel  string `envconfig:"OPENAI_MODEL"`

	// In case it is possible to deterministically classify an incident (the alert bot always uses
	// the same message format), we can use this to classify the incident without using the OpenAI API.
	IncidentBinary string `split_words:"true"`
}

type ClassifierWorker struct {
	river.WorkerDefaults[background.ClassifierArgs]

	incidentBinary string
	openaiClient   *openai.Client
	openaiModel    string
	bot            *internal.Bot
}

func New(ctx context.Context, c Config, bot *internal.Bot) (river.Worker[background.ClassifierArgs], error) {
	if c.IncidentBinary != "" {
		if _, err := exec.LookPath(c.IncidentBinary); err != nil {
			return nil, err
		}
	}

	var openaiClient *openai.Client
	if c.OpenAIModel != "" {
		openaiClient := openai.NewClient(option.WithBaseURL(c.OpenAIURL), option.WithAPIKey(c.OpenAIAPIKey))
		if _, err := openaiClient.Models.Get(ctx, c.OpenAIModel); err != nil {
			return nil, err
		}
	}

	return &ClassifierWorker{
		incidentBinary: c.IncidentBinary,
		openaiClient:   openaiClient,
		openaiModel:    c.OpenAIModel,
		bot:            bot,
	}, nil
}

type Action string
type Priority string

const (
	ActionOpenIncident  Action = "open_incident"
	ActionCloseIncident Action = "close_incident"

	PriorityHigh Priority = "HIGH"
	PriorityLow  Priority = "LOW"
)

func (a *Action) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	switch s {
	case "open_incident":
		*a = ActionOpenIncident
	case "close_incident":
		*a = ActionCloseIncident
	default:
		return fmt.Errorf("unknown action: %s", s)
	}

	return nil
}

func (p *Priority) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	switch s {
	case "HIGH":
		*p = PriorityHigh
	case "LOW":
		*p = PriorityLow
	default:
		return fmt.Errorf("unknown priority: %s", s)
	}

	return nil
}

type IncidentAction struct {
	Action  Action `json:"action"`
	Alert   string `json:"alert"`
	Service string `json:"service"`

	// Only used for open_incident.
	Priority Priority `json:"priority,omitempty"`
}

func (w *ClassifierWorker) Work(ctx context.Context, job *river.Job[background.ClassifierArgs]) error {
	msg, err := w.bot.GetMessage(ctx, job.Args.ChannelID, job.Args.SlackTS)
	if err != nil {
		return err
	}

	if w.incidentBinary != "" {
		action, err := runIncidentBinary(w.incidentBinary, msg.Attrs)
		if err != nil {
			log.Printf("failed to classify incident with binary: %v", err)
		}

		log.Printf("classified incident: %v\n", action)
		if err := processIncidentAction(ctx, w.bot, msg, action); err != nil {
			return fmt.Errorf("failed to process incident action: %w", err)
		}
	}

	// TODO: Use OpenAI API to classify incidents, bot updates and human interactions instead of this hard-coded behavior.
	subType := ""
	if msg.Attrs.Upstream != nil {
		subType = msg.Attrs.Upstream.SubType
	} else {
		subType = msg.Attrs.Message.SubType
	}

	if subType == "bot_message" {
		botName := ""
		if msg.Attrs.Upstream != nil {
			botName = msg.Attrs.Upstream.Username
		} else {
			botName = msg.Attrs.Message.Username
		}

		return w.bot.TagAsBotNotification(ctx, msg.ChannelID, msg.SlackTs, botName)
	}

	userID := ""
	if msg.Attrs.Upstream != nil {
		userID = msg.Attrs.Upstream.User
	} else {
		userID = msg.Attrs.Message.User
	}

	return w.bot.TagAsUserMessage(ctx, msg.ChannelID, msg.SlackTs, userID)
}

func processIncidentAction(
	ctx context.Context,
	bot *internal.Bot,
	msg schema.Message,
	action *IncidentAction,
) error {
	t, err := slack.TsToTime(msg.SlackTs)
	if err != nil {
		return fmt.Errorf("failed to parse Slack timestamp: %w", err)
	}

	tz := pgtype.Timestamptz{Time: t, Valid: true}

	switch action.Action {
	case ActionOpenIncident:
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
	case ActionCloseIncident:
		if err := bot.CloseIncident(ctx, msg.ChannelID, msg.SlackTs, action.Alert, action.Service, tz); err != nil {
			return fmt.Errorf("failed to close incident: %w", err)
		}
	}

	return nil
}

func runIncidentBinary(binaryPath string, input dto.MessageAttrs) (*IncidentAction, error) {
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

	var output IncidentAction
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return nil, fmt.Errorf("failed to parse output: %w", err)
	}

	return &output, nil
}
