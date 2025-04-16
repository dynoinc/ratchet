package recent_activity

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pgvector/pgvector-go"

	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type Activity struct {
	ChannelID    string
	Ts           string
	Attrs        dto.MessageAttrs
	SemanticRank int64
	LexicalRank  int64
	RRFScore     float64
}

func Get(
	ctx context.Context,
	qtx *schema.Queries,
	llmClient llm.Client,
	lexicalQuery string,
	semanticQuery string,
	interval time.Duration,
	botID string,
) ([]Activity, error) {
	queryEmbedding, err := llmClient.GenerateEmbedding(ctx, "search_query", semanticQuery)
	if err != nil {
		return nil, fmt.Errorf("generating embedding: %w", err)
	}

	embedding := pgvector.NewVector(queryEmbedding)
	updates, err := qtx.GetLatestServiceUpdates(ctx, schema.GetLatestServiceUpdatesParams{
		QueryText:      lexicalQuery,
		QueryEmbedding: &embedding,
		Interval:       pgtype.Interval{Microseconds: interval.Microseconds(), Valid: true},
		BotID:          botID,
	})
	if err != nil {
		return nil, fmt.Errorf("getting latest service updates: %w", err)
	}

	messages := make([]Activity, len(updates))
	for i, update := range updates {
		var attrs dto.MessageAttrs
		if err := json.Unmarshal(update.Attrs, &attrs); err != nil {
			return nil, fmt.Errorf("unmarshalling message attrs: %w", err)
		}

		messages[i] = Activity{
			ChannelID:    update.ChannelID,
			Ts:           update.Ts,
			Attrs:        attrs,
			SemanticRank: update.SemanticRank,
			LexicalRank:  update.LexicalRank,
			RRFScore:     update.CRrfScore,
		}
	}

	return messages, nil
}

// GetDebug performs the same search as Get but returns additional debug information
func GetDebug(
	ctx context.Context,
	qtx *schema.Queries,
	llmClient llm.Client,
	lexicalQuery string,
	semanticQuery string,
	interval time.Duration,
	botID string,
) ([]schema.DebugGetLatestServiceUpdatesRow, error) {
	queryEmbedding, err := llmClient.GenerateEmbedding(ctx, "search_query", semanticQuery)
	if err != nil {
		return nil, fmt.Errorf("generating embedding: %w", err)
	}

	embedding := pgvector.NewVector(queryEmbedding)
	updates, err := qtx.DebugGetLatestServiceUpdates(ctx, schema.DebugGetLatestServiceUpdatesParams{
		QueryText:      lexicalQuery,
		QueryEmbedding: &embedding,
		Interval:       pgtype.Interval{Microseconds: interval.Microseconds(), Valid: true},
		BotID:          botID,
	})
	if err != nil {
		return nil, fmt.Errorf("getting latest service updates: %w", err)
	}

	return updates, nil
}
