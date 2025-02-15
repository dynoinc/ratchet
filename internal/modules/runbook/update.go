package runbook

import (
	"context"
	"errors"
	"fmt"

	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
	"github.com/jackc/pgx/v5"
)

func Update(
	ctx context.Context,
	qtx *schema.Queries,
	llmClient *llm.Client,
	serviceName, alertName string,
	forceRecreate bool,
) (string, error) {
	msgs, err := qtx.GetThreadMessagesByServiceAndAlert(ctx, schema.GetThreadMessagesByServiceAndAlertParams{
		Service: serviceName,
		Alert:   alertName,
	})
	if err != nil {
		return "", fmt.Errorf("getting thread messages: %w", err)
	}

	if len(msgs) == 0 {
		return "", nil
	}

	// get current runbook
	runbook, err := qtx.GetRunbook(ctx, schema.GetRunbookParams{
		ServiceName: serviceName,
		AlertName:   alertName,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("getting runbook: %w", err)
	}

	var updatedRunbook string
	if runbook == (schema.IncidentRunbook{}) || forceRecreate {
		// create new runbook from scratch
		updatedRunbook, err = llmClient.CreateRunbook(ctx, serviceName, alertName, msgs)
		if err != nil {
			return "", fmt.Errorf("creating runbook: %w", err)
		}
	} else {
		// update existing runbook with new messages
		updatedRunbook, err = llmClient.UpdateRunbook(ctx, runbook, msgs)
		if err != nil {
			return "", fmt.Errorf("updating runbook: %w", err)
		}
	}

	if updatedRunbook != "" {
		if _, err := qtx.CreateRunbook(ctx, dto.RunbookAttrs{
			ServiceName: serviceName,
			AlertName:   alertName,
			Runbook:     updatedRunbook,
		}); err != nil {
			return "", fmt.Errorf("writing updated runbook: %w", err)
		}
	}

	return updatedRunbook, nil
}
