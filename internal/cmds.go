package internal

import (
	"context"
	"math"

	"github.com/dynoinc/ratchet/internal/llm"
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
	llmClient  *llm.Client
	embeddings map[cmd][]float64
}

func prepareCommands(ctx context.Context, llmClient *llm.Client) (*commands, error) {
	m := make(map[cmd][]float64)
	for msg, cmd := range cmds {
		embedding, err := llmClient.GenerateEmbedding(ctx, msg)
		if err != nil {
			return nil, err
		}

		f64s := make([]float64, len(embedding))
		for i, v := range embedding {
			f64s[i] = float64(v)
		}

		m[cmd] = f64s
	}
	return &commands{llmClient: llmClient, embeddings: m}, nil
}

func (c *commands) findCommand(ctx context.Context, message string) (cmd, error) {
	embedding, err := c.llmClient.GenerateEmbedding(ctx, message)
	if err != nil {
		return cmdNone, err
	}

	// Convert embedding to float64 slice
	f64s := make([]float64, len(embedding))
	for i, v := range embedding {
		f64s[i] = float64(v)
	}

	bestScore := 0.0
	bestMatch := cmdNone
	for cmd, embedding := range c.embeddings {
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

	if bestScore > 0.8 {
		return cmdNone, nil
	}

	return bestMatch, nil
}
