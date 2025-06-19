package docupdate

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/pgvector/pgvector-go"
	"github.com/slack-go/slack"

	"github.com/dynoinc/ratchet/internal/docs"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

func computeEmbedding(
	ctx context.Context,
	queries *schema.Queries,
	llm llm.Client,
	channelID string,
	ts string,
	text string,
) (pgvector.Vector, string, error) {
	channelInfo, err := queries.GetChannel(ctx, channelID)
	if err != nil {
		return pgvector.Vector{}, "", fmt.Errorf("failed to get channel info: %w", err)
	}
	slackMsg, err := queries.GetMessage(ctx, schema.GetMessageParams{
		ChannelID: channelID,
		Ts:        ts,
	})
	if err != nil {
		return pgvector.Vector{}, "", fmt.Errorf("failed to get message: %w", err)
	}

	threadMsgs, err := queries.GetThreadMessages(ctx, schema.GetThreadMessagesParams{
		ChannelID: channelID,
		ParentTs:  ts,
		LimitVal:  10,
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
	embedding, err := llm.GenerateEmbedding(ctx, "documentation", combinedText)
	if err != nil {
		return pgvector.Vector{}, "", fmt.Errorf("failed to generate embedding: %w", err)
	}

	return pgvector.NewVector(embedding), threadMsgsText, nil
}

func DebugCompute(
	ctx context.Context,
	queries *schema.Queries,
	llm llm.Client,
	channelID string,
	ts string,
	text string,
) ([]schema.DebugGetDocumentForUpdateRow, error) {
	embedding, _, err := computeEmbedding(ctx, queries, llm, channelID, ts, text)
	if err != nil {
		return nil, fmt.Errorf("failed to compute embedding: %w", err)
	}

	docs, err := queries.DebugGetDocumentForUpdate(ctx, &embedding)
	if err != nil {
		return nil, fmt.Errorf("failed to get document for update: %w", err)
	}

	return docs, nil
}

func Compute(
	ctx context.Context,
	queries *schema.Queries,
	llm llm.Client,
	channelID string,
	ts string,
	text string,
) (schema.DocumentationDoc, string, error) {
	var docToUpdate schema.DocumentationDoc
	for _, word := range strings.Split(text, " ") {
		if strings.HasSuffix(word, ".md") || strings.HasSuffix(word, ".txt") {
			docs, err := queries.GetDocumentByPathSuffix(ctx, &word)
			if err == nil && len(docs) == 1 {
				docToUpdate = docs[0]
				break
			}
		}
	}

	embedding, threadMsgsText, err := computeEmbedding(ctx, queries, llm, channelID, ts, text)
	if err != nil {
		return schema.DocumentationDoc{}, "", fmt.Errorf("failed to compute embedding: %w", err)
	}

	if docToUpdate == (schema.DocumentationDoc{}) {
		doc, err := queries.GetDocumentForUpdate(ctx, &embedding)
		if err != nil {
			return schema.DocumentationDoc{}, "", fmt.Errorf("failed to get document for update: %w", err)
		}

		docToUpdate = doc
	}

	// Send doc and faq to LLM with all the slack messages and ask it to return either updated doc or faq
	slog.Info("Generating documentation update", "doc", docToUpdate.Path, "text", text)
	updatedDoc, err := llm.GenerateDocumentationUpdate(ctx, docToUpdate.Content, threadMsgsText)
	if err != nil {
		return schema.DocumentationDoc{}, "", fmt.Errorf("failed to generate documentation update: %w", err)
	}

	return docToUpdate, updatedDoc, nil
}

func Post(
	ctx context.Context,
	queries *schema.Queries,
	llm llm.Client,
	slackIntegration slack_integration.Integration,
	docsConfig *docs.Config,
	channelID string,
	ts string,
	text string,
) error {
	if docsConfig == nil {
		return fmt.Errorf("documentation config not available")
	}

	doc, updatedDoc, err := Compute(ctx, queries, llm, channelID, ts, text)
	if err != nil {
		return fmt.Errorf("failed to compute: %w", err)
	}

	var source docs.Source
	for _, s := range docsConfig.Sources {
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

	err = slackIntegration.PostThreadReply(ctx, channelID, ts, blocks...)
	if err != nil {
		return fmt.Errorf("failed to post thread reply: %w", err)
	}

	return nil
}

func Generate(
	ctx context.Context,
	queries *schema.Queries,
	llm llm.Client,
	docsConfig *docs.Config,
	channelID string,
	ts string,
	text string,
) (string, error) {
	if docsConfig == nil {
		return "", fmt.Errorf("documentation config not available")
	}

	doc, updatedDoc, err := Compute(ctx, queries, llm, channelID, ts, text)
	if err != nil {
		return "", fmt.Errorf("failed to compute: %w", err)
	}

	var source docs.Source
	for _, s := range docsConfig.Sources {
		if s.URL() == doc.Url {
			source = s
			break
		}
	}

	if source == (docs.Source{}) {
		return "", fmt.Errorf("no matching source found for document")
	}

	url, err := source.Suggest(ctx, doc.Path, doc.Revision, updatedDoc)
	if err != nil {
		return "", fmt.Errorf("failed to suggest documentation update: %w", err)
	}

	return url, nil
}
