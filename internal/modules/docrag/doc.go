package docrag

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/pgvector/pgvector-go"
	"github.com/slack-go/slack"

	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
)

func Post(
	ctx context.Context,
	queries *schema.Queries,
	llmClient llm.Client,
	slackIntegration slack_integration.Integration,
	channelID string,
	ts string,
	text string,
) error {
	answer, links, err := Compute(ctx, queries, llmClient, slackIntegration, channelID, ts, text)
	if err != nil {
		return fmt.Errorf("generating answer: %w", err)
	}

	err = slackIntegration.PostThreadReply(ctx, channelID, ts, formatResponse(answer, links)...)
	if err != nil {
		return fmt.Errorf("posting message: %w", err)
	}

	return nil
}

func makeEmbeddingQuery(channelName string, question string, botRequest string) string {
	return fmt.Sprintf("In a slack channel named %s, there is a message with the following text: %s\n\nThe user was given the following request: %s",
		channelName,
		question,
		botRequest)
}

func Compute(
	ctx context.Context,
	queries *schema.Queries,
	llmClient llm.Client,
	slackIntegration slack_integration.Integration,
	channelID string,
	ts string,
	text string,
) (string, []string, error) {
	channelInfo, err := queries.GetChannel(ctx, channelID)
	if err != nil {
		return "", nil, fmt.Errorf("failed to get channel info: %w", err)
	}
	msg, err := queries.GetMessage(ctx, schema.GetMessageParams{
		ChannelID: channelID,
		Ts:        ts,
	})
	if err != nil {
		return "", nil, fmt.Errorf("getting message: %w", err)
	}
	threadMsgs, err := queries.GetThreadMessages(ctx, schema.GetThreadMessagesParams{
		ChannelID: channelID,
		ParentTs:  ts,
		BotID:     slackIntegration.BotUserID(),
	})
	if err != nil {
		return "", nil, fmt.Errorf("failed to get thread messages: %w", err)
	}

	conversation := []string{
		msg.Attrs.Message.Text,
	}
	for _, msg := range threadMsgs {
		conversation = append(conversation, msg.Attrs.Message.Text)
	}

	answer, links, err := Answer(ctx, queries, llmClient, channelInfo.Attrs.Name, conversation, text)
	if err != nil {
		return "", nil, fmt.Errorf("generating answer: %w", err)
	}

	return answer, links, nil
}

func Answer(
	ctx context.Context,
	queries *schema.Queries,
	llmClient llm.Client,
	channelName string,
	conversation []string,
	botRequest string,
) (string, []string, error) {
	combinedText := makeEmbeddingQuery(channelName, strings.Join(conversation, "\n"), botRequest)
	embedding, err := llmClient.GenerateEmbedding(ctx, "documentation", combinedText)
	if err != nil {
		return "", nil, fmt.Errorf("generating embedding: %w", err)
	}
	vec := pgvector.NewVector(embedding)

	docs, err := queries.GetClosestDocs(ctx, schema.GetClosestDocsParams{
		Embedding: &vec,
		LimitVal:  5,
	})
	if err != nil {
		return "", nil, fmt.Errorf("getting closest docs: %w", err)
	}

	contents := make([]string, 0, len(docs))
	for _, doc := range docs {
		contents = append(contents, doc.Content)
	}

	links := make([]string, 0, len(docs))
	for _, doc := range docs {
		links = append(links, fmt.Sprintf("%s/blob/%s/%s", doc.Url, doc.Revision, doc.Path))
	}

	query := fmt.Sprintf("In a slack channel named %s, there is a conversation happening. The user was given the following request: %s\n\nThe conversation is as follows: %s",
		channelName,
		botRequest,
		strings.Join(conversation, "\n"),
	)
	slog.InfoContext(ctx, "Generating documentation response", "query", query, "links", links)
	answer, err := llmClient.GenerateDocumentationResponse(ctx, query, contents)
	if err != nil {
		return "", nil, fmt.Errorf("generating documentation response: %w", err)
	}

	return answer, links, nil
}

func Debug(
	ctx context.Context,
	queries *schema.Queries,
	llmClient llm.Client,
	channelName string,
	question string,
	botRequest string,
) ([]schema.DebugGetClosestDocsRow, error) {
	combinedText := makeEmbeddingQuery(channelName, question, botRequest)
	embedding, err := llmClient.GenerateEmbedding(ctx, "search", combinedText)
	if err != nil {
		return nil, fmt.Errorf("generating embedding: %w", err)
	}
	vec := pgvector.NewVector(embedding)

	docs, err := queries.DebugGetClosestDocs(ctx, schema.DebugGetClosestDocsParams{
		Embedding: &vec,
		LimitVal:  5,
	})
	if err != nil {
		return nil, fmt.Errorf("getting closest docs: %w", err)
	}

	return docs, nil
}

func formatResponse(answer string, links []string) []slack.Block {
	blocks := []slack.Block{
		slack.NewSectionBlock(slack.NewTextBlockObject(slack.MarkdownType, answer, false, false), nil, nil),
	}

	if len(links) > 0 {
		// Create a section with clickable links
		formattedLinks := make([]string, 0, len(links))
		for _, link := range links {
			formattedLinks = append(formattedLinks, fmt.Sprintf("<%s|%s>", link, filepath.Base(link)))
		}

		blocks = append(blocks, slack.NewDividerBlock())
		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, "*Sources:*\n"+strings.Join(formattedLinks, "\n"), false, false),
			nil, nil,
		))
	}

	blocks = append(blocks, slack_integration.CreateSignatureBlock("Documentation")...)
	return blocks
}
