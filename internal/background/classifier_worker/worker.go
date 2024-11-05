package classifier_worker

import (
	"context"
	"os/exec"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/riverqueue/river"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
)

type Config struct {
	OpenAIAPIKey string `envconfig:"CLASSIFIER_OPENAI_API_KEY" default:"fake-classifier-key"`
	OpenAIURL    string `envconfig:"CLASSIFIER_OPENAI_URL" default:"http://localhost:11434/v1/"`
	OpenAIModel  string `envconfig:"CLASSIFIER_OPENAI_MODEL" default:"mistral"`

	// In case it is possible to deterministically classify an incident (the alert bot always uses
	// the same message format), we can use this to classify the incident without using the OpenAI API.
	ClassifierIncidentBinary string `split_words:"true"`
}

type ClassifierWorker struct {
	river.WorkerDefaults[background.ClassifierArgs]

	incidentBinary string
	openaiClient   *openai.Client
	openaiModel    string
	bot            *internal.Bot
}

func New(ctx context.Context, c Config, bot *internal.Bot) (*ClassifierWorker, error) {
	if c.ClassifierIncidentBinary != "" {
		if _, err := exec.LookPath(c.ClassifierIncidentBinary); err != nil {
			return nil, err
		}
	}

	openaiClient := openai.NewClient(option.WithBaseURL(c.OpenAIURL), option.WithAPIKey(c.OpenAIAPIKey))
	if _, err := openaiClient.Models.Get(ctx, c.OpenAIModel); err != nil {
		return nil, err
	}

	return &ClassifierWorker{
		incidentBinary: c.ClassifierIncidentBinary,
		openaiClient:   openaiClient,
		openaiModel:    c.OpenAIModel,
		bot:            bot,
	}, nil
}

func (w *ClassifierWorker) Work(ctx context.Context, job *river.Job[background.ClassifierArgs]) error {

	return nil
}
