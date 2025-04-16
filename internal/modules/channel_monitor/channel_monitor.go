package channel_monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"text/template"

	"github.com/qri-io/jsonschema"
	"github.com/slack-go/slack"
	"gopkg.in/yaml.v3"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type Config struct {
	ConfigFile string `split_words:"true"`
}

type config = map[string]*entry

type entry struct {
	ChannelID      string             `yaml:"channel_id" json:"channel_id"`
	Prompt         string             `yaml:"prompt" json:"prompt"`
	PromptTemplate *template.Template `yaml:"-" json:"-"`
	ResultSchema   *jsonschema.Schema `yaml:"result_schema" json:"result_schema"`
	Executable     string             `yaml:"executable" json:"executable"`
	ExecutableArgs []string           `yaml:"executable_args" json:"executable_args"`
}

type promptData struct {
	Message dto.SlackMessage
}

type executableStdInData struct {
	Slug      string           `json:"slug"`
	ChannelID string           `json:"channel_id"`
	SlackTS   string           `json:"slack_ts"`
	LLMOutput string           `json:"llm_output"`
	Message   dto.SlackMessage `json:"message"`
}

type executableStdOutData struct {
	DirectMessages  []directMessage   `json:"direct_messages"`
	ChannelMessages []*channelMessage `json:"channel_messages"`
}

type directMessage struct {
	Email string `json:"email"`
	Text  string `json:"text"`
}

type channelMessage struct {
	ChannelID      string `json:"channel_id"`
	Text           string `json:"text"`
	SlackTimestamp string `json:"slack_ts"`
}

type ChannelMonitor struct {
	bot              *internal.Bot
	slackIntegration slack_integration.Integration
	llmClient        llm.Client
	cfg              config
}

func (c *ChannelMonitor) Name() string {
	return "channel_monitor"
}

func New(
	c Config,
	bot *internal.Bot,
	slackIntegration slack_integration.Integration,
	llmClient llm.Client,
) *ChannelMonitor {
	cfg := config{}
	if c.ConfigFile != "" {
		b, err := os.ReadFile(c.ConfigFile)
		if err != nil {
			slog.Error("reading config file", "error", err)
		}
		cfg, err = parseConfig(b)
		if err != nil {
			slog.Error("loading config file", "error", err)
		}
	}
	return &ChannelMonitor{
		bot:              bot,
		slackIntegration: slackIntegration,
		llmClient:        llmClient,
		cfg:              cfg,
	}
}

func parseConfig(b []byte) (config, error) {
	// Takes user input as yaml to make multi-line prompts easier, but json schema requires parsing from json
	parsedYaml := &map[string]interface{}{}
	if err := yaml.Unmarshal(b, parsedYaml); err != nil {
		return nil, err
	}
	marshaled, err := json.Marshal(parsedYaml)
	if err != nil {
		return nil, err
	}
	parsed := &map[string]*entry{}
	if err := json.Unmarshal(marshaled, parsed); err != nil {
		return nil, err
	}
	for slug, entry := range *parsed {
		if entry.ChannelID == "" {
			return nil, fmt.Errorf("missing channel_id for entry %s", slug)
		}
		if entry.Prompt == "" {
			return nil, fmt.Errorf("missing prompt for entry %s", slug)
		}
		if entry.Executable == "" {
			return nil, fmt.Errorf("missing executable for entry %s", slug)
		}
		if _, err := exec.LookPath(entry.Executable); err != nil {
			return nil, fmt.Errorf("looking up executable for entry %s: %w", slug, err)
		}
		tmpl, err := template.New(slug).Parse(entry.Prompt)
		if err != nil {
			return nil, err
		}
		entry.PromptTemplate = tmpl
	}
	return *parsed, nil
}

func (c *ChannelMonitor) Handle(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error {
	if msg.Message.SubType != "" {
		return nil
	}
	for slug, entry := range c.cfg {
		if entry.ChannelID != channelID {
			continue
		}
		slog.Debug("found matching channel", "channel_id", entry.ChannelID, "slug", slug)
		if err := c.handleMessage(ctx, slug, entry, slackTS, msg); err != nil {
			return fmt.Errorf("handling message in channel %s: %w", channelID, err)
		}
	}
	return nil
}

func (c *ChannelMonitor) handleMessage(ctx context.Context, slug string, entry *entry, slackTS string, msg dto.MessageAttrs) error {
	data := promptData{msg.Message}
	var prompt bytes.Buffer
	err := entry.PromptTemplate.Execute(&prompt, data)
	if err != nil {
		return fmt.Errorf("executing prompt template: %w", err)
	}
	lmmOutput, _, err := c.llmClient.RunJSONModePrompt(ctx, prompt.String(), entry.ResultSchema)
	if err != nil {
		return fmt.Errorf("running prompt: %w", err)
	}
	output, err := c.runExecutable(slug, entry, slackTS, lmmOutput, msg)
	if err != nil {
		return err
	}
	slog.Debug("command output", "output", output)
	outputData := executableStdOutData{}
	if err := json.Unmarshal([]byte(output), &outputData); err != nil {
		return fmt.Errorf("unmarshalling command output: %w", err)
	}
	return c.doOutputActions(ctx, outputData)
}

func (c *ChannelMonitor) runExecutable(slug string, entry *entry, slackTS string, lmmOutput string, msg dto.MessageAttrs) (string, error) {
	stdInData := executableStdInData{
		Slug:      slug,
		ChannelID: entry.ChannelID,
		SlackTS:   slackTS,
		LLMOutput: lmmOutput,
		Message:   msg.Message,
	}
	stdInDataBytes, err := json.Marshal(stdInData)
	if err != nil {
		return "", fmt.Errorf("marshalling stdin data: %w", err)
	}
	var stdoutBuffer bytes.Buffer
	cmd := exec.Command(entry.Executable, entry.ExecutableArgs...)
	cmd.Stdout = &stdoutBuffer
	cmd.Stdin = bytes.NewReader(stdInDataBytes)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("command execution failed: %w", err)
	}
	output := stdoutBuffer.String()
	return output, nil
}

func (c *ChannelMonitor) doOutputActions(ctx context.Context, outputData executableStdOutData) error {
	for _, dm := range outputData.DirectMessages {
		userID, err := c.slackIntegration.GetUserIDByEmail(ctx, dm.Email)
		if err != nil {
			slog.Warn("getting user ID by email", "error", err)
			continue
		}
		blocks := []slack.Block{
			slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, dm.Text, false, false), nil, nil),
		}
		blocks = append(blocks, slack_integration.CreateSignatureBlock("Channel Monitor")...)
		err = c.slackIntegration.PostMessage(ctx, userID, blocks...)
		if err != nil {
			return fmt.Errorf("posting direct message: %w", err)
		}
	}
	for _, message := range outputData.ChannelMessages {
		blocks := []slack.Block{
			slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, message.Text, false, false), nil, nil),
		}
		blocks = append(blocks, slack_integration.CreateSignatureBlock("Channel Monitor")...)
		if message.SlackTimestamp == "" {
			err := c.slackIntegration.PostMessage(ctx, message.ChannelID, blocks...)
			if err != nil {
				return fmt.Errorf("posting message: %w", err)
			}
		} else {
			err := c.slackIntegration.PostThreadReply(ctx, message.ChannelID, message.SlackTimestamp, blocks...)
			if err != nil {
				return fmt.Errorf("posting thread reply: %w", err)
			}
		}
	}
	return nil
}
