package recent_activity

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/pgvector/pgvector-go"
)

func Get(
	ctx context.Context,
	qtx *schema.Queries,
	llmClient *llm.Client,
	serviceName, alertName string,
	interval time.Duration,
	botID string,
) ([]schema.MessagesV2, error) {
	queryText := fmt.Sprintf("%s %s", serviceName, alertName)
	queryEmbedding, err := llmClient.GenerateEmbedding(ctx, "search_query", queryText)
	if err != nil {
		return nil, fmt.Errorf("generating embedding: %w", err)
	}

	embedding := pgvector.NewVector(queryEmbedding)
	updates, err := qtx.GetLatestServiceUpdates(ctx, schema.GetLatestServiceUpdatesParams{
		QueryText:      queryText,
		QueryEmbedding: &embedding,
		Interval:       pgtype.Interval{Microseconds: interval.Microseconds(), Valid: true},
		BotID:          botID,
	})
	if err != nil {
		return nil, fmt.Errorf("getting latest service updates: %w", err)
	}

	messages := make([]schema.MessagesV2, len(updates))
	for i, update := range updates {
		var attrs dto.MessageAttrs
		if err := json.Unmarshal(update.Attrs, &attrs); err != nil {
			return nil, fmt.Errorf("unmarshalling message attrs: %w", err)
		}

		messages[i] = schema.MessagesV2{
			ChannelID: update.ChannelID,
			Ts:        update.Ts,
			Attrs:     attrs,
		}
	}

	return messages, nil
}
