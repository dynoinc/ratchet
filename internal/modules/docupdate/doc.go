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

func (u *DocUpdater) computeEmbedding(ctx context.Context, channelID string, threadTS string, text string) (pgvector.Vector, string, error) {
	queries := schema.New(u.db)
	channelInfo, err := queries.GetChannel(ctx, channelID)
	if err != nil {
		return pgvector.Vector{}, "", fmt.Errorf("failed to get channel info: %w", err)
	}
	slackMsg, err := queries.GetMessage(ctx, schema.GetMessageParams{
		ChannelID: channelID,
		Ts:        threadTS,
	})
	if err != nil {
		return pgvector.Vector{}, "", fmt.Errorf("failed to get message: %w", err)
	}

	threadMsgs, err := queries.GetThreadMessages(ctx, schema.GetThreadMessagesParams{
		ChannelID: channelID,
		ParentTs:  threadTS,
	})
	if err != nil {
		return pgvector.Vector{}, "", fmt.Errorf("failed to get thread messages: %w", err)
	}

	threadMsgsText := slackMsg.Attrs.Message.Text + "\n\n"
	for _, msg := range threadMsgs {
		threadMsgsText += msg.Attrs.Message.Text + "\n\n"
	}

	combinedText := fmt.Sprintf("In a slack channel named %s, there is a message with the following text: %s\n\nThe user has requested the following documentation update: %s",
		channelInfo.Attrs.Name,
		slackMsg.Attrs.Message.Text,
		text)
	embedding, err := u.llm.GenerateEmbedding(ctx, "documentation", combinedText)
	if err != nil {
		return pgvector.Vector{}, "", fmt.Errorf("failed to generate embedding: %w", err)
	}

	return pgvector.NewVector(embedding), threadMsgsText, nil
}
func (u *DocUpdater) DebugCompute(
	ctx context.Context,
	channelID string,
	threadTS string,
	text string,
) ([]schema.DebugGetDocumentForUpdateRow, error) {
	queries := schema.New(u.db)
	embedding, _, err := u.computeEmbedding(ctx, channelID, threadTS, text)
	if err != nil {
		return nil, fmt.Errorf("failed to compute embedding: %w", err)
	}

	docs, err := queries.DebugGetDocumentForUpdate(ctx, &embedding)
	if err != nil {
		return nil, fmt.Errorf("failed to get document for update: %w", err)
	}

	return docs, nil
}

func (u *DocUpdater) Compute(
	ctx context.Context,
	channelID string,
	threadTS string,
	text string,
) (schema.DocumentationDoc, string, error) {
	queries := schema.New(u.db)
	embedding, threadMsgsText, err := u.computeEmbedding(ctx, channelID, threadTS, text)
	if err != nil {
		return schema.DocumentationDoc{}, "", fmt.Errorf("failed to compute embedding: %w", err)
	}

	doc, err := queries.GetDocumentForUpdate(ctx, &embedding)
	if err != nil {
		return schema.DocumentationDoc{}, "", fmt.Errorf("failed to get document for update: %w", err)
	}

	// Send doc and faq to LLM with all the slack messages and ask it to return either updated doc or faq

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
