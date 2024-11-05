package classifier_worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/riverqueue/river"
	"github.com/slack-go/slack/slackevents"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

type Config struct {
	OpenAIAPIKey string `envconfig:"OPENAI_API_KEY" default:"fake-classifier-key"`
	OpenAIURL    string `envconfig:"OPENAI_URL" default:"http://localhost:11434/v1/"`
	OpenAIModel  string `envconfig:"OPENAI_MODEL" default:"mistral"`

	// In case it is possible to deterministically classify an incident (the alert bot always uses
	// the same message format), we can use this to classify the incident without using the OpenAI API.
	IncidentBinary string `split_words:"true" required:"false"`
}

type ClassifierWorker struct {
	river.WorkerDefaults[background.ClassifierArgs]

	incidentBinary string
	openaiClient   *openai.Client
	openaiModel    string
	bot            *internal.Bot
}

func New(ctx context.Context, c Config, bot *internal.Bot) (*ClassifierWorker, error) {
	if c.IncidentBinary != "" {
		if _, err := exec.LookPath(c.IncidentBinary); err != nil {
			return nil, err
		}
	}

	openaiClient := openai.NewClient(option.WithBaseURL(c.OpenAIURL), option.WithAPIKey(c.OpenAIAPIKey))
	if _, err := openaiClient.Models.Get(ctx, c.OpenAIModel); err != nil {
		return nil, err
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
		log.Printf("processing message: %s\n", msg.Attrs.Upstream.Text)
		action, err := runIncidentBinary(w.incidentBinary, msg.Attrs.Upstream)
		if err != nil {
			log.Printf("failed to classify incident with binary: %v", err)
		}

		log.Printf("classified incident: %v\n", action)
		if err := w.processIncidentAction(ctx, msg, action); err != nil {
			return fmt.Errorf("failed to process incident action: %w", err)
		}
	} else {
		log.Printf("no incident binary found, skipping classification")
	}

	// TODO: Use OpenAI API to classify incidents, bot updates and human interactions.

	return nil
}

func (w *ClassifierWorker) processIncidentAction(
	ctx context.Context,
	msg schema.Message,
	action *IncidentAction,
) error {
	switch action.Action {
	case ActionOpenIncident:
		_, err := w.bot.OpenIncident(ctx, schema.OpenIncidentParams{
			ChannelID: msg.ChannelID,
			SlackTs:   msg.SlackTs,
			Alert:     action.Alert,
			Service:   action.Service,
			Priority:  string(action.Priority),
		})
		if err != nil {
			return fmt.Errorf("failed to open incident: %w", err)
		}
	case ActionCloseIncident:
		if err := w.bot.CloseIncident(ctx, action.Alert, action.Service); err != nil {
			return fmt.Errorf("failed to close incident: %w", err)
		}
	}

	return nil
}

func runIncidentBinary(binaryPath string, input slackevents.MessageEvent) (*IncidentAction, error) {
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
