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
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type Config struct {
	ConfigFile string `split_words:"true"`
}

type config = map[string]*Entry

type Entry struct {
	ChannelID      string             `yaml:"channel_id" json:"channel_id"`
	Prompt         string             `yaml:"prompt" json:"prompt"`
	PromptTemplate *template.Template `yaml:"-" json:"-"`
	ResultSchema   *jsonschema.Schema `yaml:"result_schema" json:"result_schema"`
	Executable     string             `yaml:"executable" json:"executable"`
	ExecutableArgs []string           `yaml:"executable_args" json:"executable_args"`
}

type PromptData struct {
	Message dto.SlackMessage
}

type ExecutableStdInData struct {
	Slug      string           `json:"slug"`
	ChannelID string           `json:"channel_id"`
	SlackTS   string           `json:"slack_ts"`
	LLMOutput string           `json:"llm_output"`
	Message   dto.SlackMessage `json:"message"`
}

type ExecutableStdOutData struct {
	DirectMessages  []DirectMessage   `json:"direct_messages"`
	ChannelMessages []*ChannelMessage `json:"channel_messages"`
}

type DirectMessage struct {
	Email string `json:"email"`
	Text  string `json:"text"`
}

type ChannelMessage struct {
	ChannelID      string `json:"channel_id"`
	Text           string `json:"text"`
	SlackTimestamp string `json:"slack_ts"`
}

type channelMonitor struct {
	bot              *internal.Bot
	slackIntegration slack_integration.Integration
	llmClient        llm.Client
	cfg              config
}

func (c *channelMonitor) Name() string {
	return "channel_monitor"
}

func New(
	c Config,
	bot *internal.Bot,
	slackIntegration slack_integration.Integration,
	llmClient llm.Client,
) *channelMonitor {
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
	return &channelMonitor{
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
	parsed := &map[string]*Entry{}
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

func (c *channelMonitor) Handle(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error {
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

func (c *channelMonitor) handleMessage(ctx context.Context, slug string, entry *Entry, slackTS string, msg dto.MessageAttrs) error {
	data := PromptData{msg.Message}
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
	outputData := ExecutableStdOutData{}
	if err := json.Unmarshal([]byte(output), &outputData); err != nil {
		return fmt.Errorf("unmarshalling command output: %w", err)
	}
	return c.doOutputActions(ctx, outputData)
}

func (c *channelMonitor) runExecutable(slug string, entry *Entry, slackTS string, lmmOutput string, msg dto.MessageAttrs) (string, error) {
	stdInData := ExecutableStdInData{
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

func (c *channelMonitor) doOutputActions(ctx context.Context, outputData ExecutableStdOutData) error {
	for _, dm := range outputData.DirectMessages {
		userID, err := c.slackIntegration.GetUserIDByEmail(ctx, dm.Email)
		if err != nil {
			slog.Warn("getting user ID by email", "error", err)
			continue
		}
		err = c.slackIntegration.PostMessage(ctx, userID, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, dm.Text, false, false), nil, nil))
		if err != nil {
			return fmt.Errorf("posting direct message: %w", err)
		}
	}
	for _, message := range outputData.ChannelMessages {
		if message.SlackTimestamp == "" {
			err := c.slackIntegration.PostMessage(ctx, message.ChannelID, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, message.Text, false, false), nil, nil))
			if err != nil {
				return fmt.Errorf("posting message: %w", err)
			}
		} else {
			err := c.slackIntegration.PostThreadReply(ctx, message.ChannelID, message.SlackTimestamp, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, message.Text, false, false), nil, nil))
			if err != nil {
				return fmt.Errorf("posting thread reply: %w", err)
			}
		}
	}
	return nil
}

type TestChannelMonitorReportData struct {
	Message         dto.SlackMessage
	Prompt          string
	ValidatedOutput string
	Error           string
	InvalidOutput   string
}

func TestChannelMonitorPrompt(ctx context.Context,
	db *schema.Queries,
	llmClient llm.Client,
	slackIntegration slack_integration.Integration,
	msg dto.MessageAttrs,
	channelID string,
	slackTS string,
) error {
	entry, history, err := getEntryAndHistoryForTest(ctx, slackIntegration, channelID, slackTS)
	if err != nil {
		slackIntegration.PostThreadReply(ctx, channelID, slackTS, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, fmt.Sprintf("Error getting entry and history for test: %s", err), false, false), nil, nil))
		return fmt.Errorf("getting entry and history for test: %w", err)
	}
	reportData := []*TestChannelMonitorReportData{}
	for _, msg := range history {
		dtoMessage := dto.SlackMessage{
			SubType:     msg.SubType,
			Text:        msg.Text,
			User:        msg.User,
			BotID:       msg.BotID,
			BotUsername: msg.Username,
		}
		data := PromptData{Message: dtoMessage}
		var prompt bytes.Buffer
		err := entry.PromptTemplate.Execute(&prompt, data)
		if err != nil {
			slackIntegration.PostThreadReply(ctx, channelID, slackTS, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, fmt.Sprintf("Error executing prompt template: %s", err), false, false), nil, nil))
			return fmt.Errorf("executing prompt template: %w", err)
		}
		validOutput, invalidOutput, err := llmClient.RunJSONModePrompt(ctx, prompt.String(), entry.ResultSchema)
		reportData = append(reportData, &TestChannelMonitorReportData{
			Message:         dtoMessage,
			Prompt:          prompt.String(),
			ValidatedOutput: validOutput,
			InvalidOutput:   invalidOutput,
			Error:           err.Error(),
		})
	}
	reportMarkdown := "# Test Channel Monitor Report\n\n"
	for _, data := range reportData {
		// Show the message
		reportMarkdown += fmt.Sprintf("### Message\n\n%s\n", data.Message.Text)
		// Always show the prompt, but behind a collapsible block
		reportMarkdown += fmt.Sprintf("\n<details><summary>Prompt</summary>\n%s</details>\n\n", data.Prompt)
		// If there is validated output, show it as json code block
		if data.ValidatedOutput != "" {
			reportMarkdown += fmt.Sprintf("### Output\n\n```json\n%s\n```\n", data.ValidatedOutput)
		}
		// If there is invalid output, show it as json code block
		if data.InvalidOutput != "" {
			reportMarkdown += fmt.Sprintf("### Invalid Output\n\n<details><summary>Invalid Output</summary>\n```json\n%s\n```\n</details>\n\n", data.InvalidOutput)
		}
		// If there is an error, show it as a code block
		if data.Error != "" {
			reportMarkdown += fmt.Sprintf("### Error\n\n```\n%s\n```\n", data.Error)
		}
		reportMarkdown += "\n----\n"
	}
	slackIntegration.PostThreadReply(ctx, channelID, slackTS, slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, reportMarkdown, false, false), nil, nil))
	return nil
}

func getEntryAndHistoryForTest(ctx context.Context, slackIntegration slack_integration.Integration, channelID string, slackTS string) (*Entry, []slack.Message, error) {
	files, err := slackIntegration.GetFiles(ctx, channelID, slackTS)
	// TODO: Reply to the thread with the error
	if err != nil {
		slog.Error("getting files", "error", err)
		return nil, nil, fmt.Errorf("getting files: %w", err)
	}
	if len(files) != 1 {
		slog.Error("no files found")
		return nil, nil, fmt.Errorf("no files found")
	}
	file := files[0]
	if file.Filetype != "yaml" && file.Filetype != "yml" {
		slog.Error("file is not a yaml file", "filename", file.Name, "mimetype", file.Mimetype)
		return nil, nil, fmt.Errorf("file %s is not a yaml file", file.Name)
	}
	fileBytes, err := slackIntegration.FetchFile(ctx, file)
	if err != nil {
		return nil, nil, fmt.Errorf("fetching file: %w", err)
	}
	parsedYaml := &map[string]interface{}{}
	if err := yaml.Unmarshal(fileBytes, parsedYaml); err != nil {
		return nil, nil, fmt.Errorf("unmarshalling yaml: %w", err)
	}
	marshaled, err := json.Marshal(parsedYaml)
	if err != nil {
		return nil, nil, fmt.Errorf("marshalling json: %w", err)
	}
	entry := &Entry{}
	if err := json.Unmarshal(marshaled, entry); err != nil {
		return nil, nil, fmt.Errorf("unmarshalling json: %w", err)
	}
	history, err := slackIntegration.GetConversationHistory(ctx, channelID, 10)
	if err != nil {
		return nil, nil, fmt.Errorf("getting conversation history: %w", err)
	}
	return entry, history, nil
}
