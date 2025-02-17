package commands

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/modules/report"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type cmd int

const (
	cmdNone cmd = iota
	cmdPostReport
)

var (
	cmds = map[string]cmd{
		"post weekly report to slack channel": cmdPostReport,
	}
)

type commands struct {
	bot              *internal.Bot
	slackIntegration slack_integration.Integration
	llmClient        llm.Client

	mu         sync.Mutex
	embeddings map[cmd][]float64
}

func New(
	bot *internal.Bot,
	slackIntegration slack_integration.Integration,
	llmClient llm.Client,
) *commands {
	return &commands{
		bot:              bot,
		slackIntegration: slackIntegration,
		llmClient:        llmClient,
	}
}

func (c *commands) Name() string {
	return "commands"
}

func (c *commands) commandEmbeddings(ctx context.Context) (map[cmd][]float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.embeddings != nil {
		return c.embeddings, nil
	}

	m := make(map[cmd][]float64)
	for msg, cmd := range cmds {
		embedding, err := c.llmClient.GenerateEmbedding(ctx, "classification", msg)
		if err != nil {
			return nil, err
		}

		f64s := make([]float64, len(embedding))
		for i, v := range embedding {
			f64s[i] = float64(v)
		}

		m[cmd] = f64s
	}

	c.embeddings = m
	return m, nil
}

func (c *commands) findCommand(ctx context.Context, msg dto.SlackMessage) (cmd, float64, error) {
	embedding, err := c.llmClient.GenerateEmbedding(ctx, "classification", msg.Text)
	if err != nil {
		return cmdNone, 0, err
	}

	cmdEmbeddings, err := c.commandEmbeddings(ctx)
	if err != nil {
		return cmdNone, 0, err
	}

	// Convert embedding to float64 slice
	f64s := make([]float64, len(embedding))
	for i, v := range embedding {
		f64s[i] = float64(v)
	}

	bestScore := 0.0
	bestMatch := cmdNone
	for cmd, embedding := range cmdEmbeddings {
		// Calculate dot product
		var dotProduct float64
		for i := 0; i < len(f64s); i++ {
			dotProduct += f64s[i] * embedding[i]
		}

		// Calculate magnitudes
		var mag1, mag2 float64
		for i := 0; i < len(f64s); i++ {
			mag1 += f64s[i] * f64s[i]
			mag2 += embedding[i] * embedding[i]
		}

		cosineSimilarity := dotProduct / (math.Sqrt(mag1) * math.Sqrt(mag2))
		cosineDistance := 1 - cosineSimilarity
		if bestMatch == cmdNone || cosineDistance < bestScore {
			bestScore = cosineDistance
			bestMatch = cmd
		}
	}

	if bestScore > 0.3 {
		return cmdNone, 0, nil
	}

	return bestMatch, bestScore, nil
}

func (c *commands) Handle(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error {
	botID := c.slackIntegration.BotUserID()
	if !strings.HasPrefix(msg.Message.Text, fmt.Sprintf("<@%s> ", botID)) {
		return nil
	}

	bestMatch, score, err := c.findCommand(ctx, msg.Message)
	if err != nil {
		return err
	}

	slog.Debug("best match", "text", msg.Message.Text, "bestMatch", bestMatch, "score", score)

	switch bestMatch {
	case cmdPostReport:
		return report.Post(ctx, schema.New(c.bot.DB), c.llmClient, c.slackIntegration, channelID)
	}

	return nil
}
