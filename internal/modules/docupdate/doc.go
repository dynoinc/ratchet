package docupdate

import (
	"context"
	"fmt"

	"github.com/slack-go/slack"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"

	"github.com/dynoinc/ratchet/internal/docs"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

type DocUpdater struct {
	c                *docs.Config
	db               *pgxpool.Pool
	llm              llm.Client
	slackIntegration slack_integration.Integration
}

func New(c *docs.Config, db *pgxpool.Pool, llm llm.Client, slack slack_integration.Integration) *DocUpdater {
	return &DocUpdater{c, db, llm, slack}
}

func (u *DocUpdater) Compute(
	ctx context.Context,
	channelID string,
	threadTS string,
	text string,
) (schema.DocumentationDoc, string, error) {
	// Generate embedding for the main thread messages + the text
	queries := schema.New(u.db)
	slackMsg, err := queries.GetMessage(ctx, schema.GetMessageParams{
		ChannelID: channelID,
		Ts:        threadTS,
	})
	if err != nil {
		return schema.DocumentationDoc{}, "", fmt.Errorf("failed to get message: %w", err)
	}

	threadMsgs, err := queries.GetThreadMessages(ctx, schema.GetThreadMessagesParams{
		ChannelID: channelID,
		ParentTs:  threadTS,
	})
	if err != nil {
		return schema.DocumentationDoc{}, "", fmt.Errorf("failed to get thread messages: %w", err)
	}

	combinedText := slackMsg.Attrs.Message.Text + "\n\n" + text
	embedding, err := u.llm.GenerateEmbedding(ctx, "documentation", combinedText)
	if err != nil {
		return schema.DocumentationDoc{}, "", fmt.Errorf("failed to generate embedding: %w", err)
	}

	vec := pgvector.NewVector(embedding)
	doc, err := queries.GetDocumentForUpdate(ctx, &vec)
	if err != nil {
		return schema.DocumentationDoc{}, "", fmt.Errorf("failed to get document for update: %w", err)
	}

	// Send doc and faq to LLM with all the slack messages and ask it to return either updated doc or faq
	threadMsgsText := ""
	for _, msg := range threadMsgs {
		threadMsgsText += msg.Attrs.Message.Text + "\n\n"
	}

	updatedDoc, err := u.llm.GenerateDocumentationUpdate(ctx, doc.Content, threadMsgsText)
	if err != nil {
		return schema.DocumentationDoc{}, "", fmt.Errorf("failed to generate documentation update: %w", err)
	}

	return doc, updatedDoc, nil
}

func (u *DocUpdater) Update(
	ctx context.Context,
	channelID string,
	threadTS string,
	text string,
) error {
	doc, updatedDoc, err := u.Compute(ctx, channelID, threadTS, text)
	if err != nil {
		return fmt.Errorf("failed to compute: %w", err)
	}

	var source docs.Source
	for _, s := range u.c.Sources {
		if s.URL() == doc.Url {
			source = s
			break
		}
	}

	if source == (docs.Source{}) {
		return nil
	}

	url, err := source.Suggest(ctx, doc.Path, doc.Revision, updatedDoc)
	if err != nil {
		return fmt.Errorf("failed to suggest documentation update: %w", err)
	}

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, fmt.Sprintf("I've created a documentation update PR: <%s|View PR>", url), false, false),
			nil, nil,
		),
	}

	blocks = append(blocks, slack_integration.CreateSignatureBlock("Doc Update")...)

	err = u.slackIntegration.PostThreadReply(ctx, channelID, threadTS, blocks...)
	if err != nil {
		return fmt.Errorf("failed to post thread reply: %w", err)
	}

	return nil
}
