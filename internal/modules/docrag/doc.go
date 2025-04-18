package docrag

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"github.com/slack-go/slack"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/llm"
	"github.com/dynoinc/ratchet/internal/modules"
	"github.com/dynoinc/ratchet/internal/slack_integration"
	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

type docRAG struct {
	bot              *internal.Bot
	slackIntegration slack_integration.Integration
	llmClient        llm.Client
}

func New(bot *internal.Bot, slackIntegration slack_integration.Integration, llmClient llm.Client) modules.Handler {
	return &docRAG{
		bot:              bot,
		slackIntegration: slackIntegration,
		llmClient:        llmClient,
	}
}

func (d *docRAG) Name() string {
	return "documentation"
}

func (d *docRAG) OnMessage(ctx context.Context, channelID string, slackTS string, msg dto.MessageAttrs) error {
	if msg.Message.BotID != "" || msg.Message.BotUsername != "" || msg.Message.Text == "" {
		return nil
	}

	channelInfo, err := schema.New(d.bot.DB).GetChannel(ctx, channelID)
	if err != nil {
		return fmt.Errorf("getting channel info: %w", err)
	}
	if !channelInfo.Attrs.DocResponsesEnabled {
		return nil
	}

	answer, links, err := Answer(ctx, msg.Message.Text, d.bot.DB, d.llmClient)
	if err != nil {
		return fmt.Errorf("generating answer: %w", err)
	}

	err = d.slackIntegration.PostThreadReply(ctx, channelID, slackTS, formatResponse(answer, links)...)
	if err != nil {
		return fmt.Errorf("posting message: %w", err)
	}

	return nil
}

func Answer(ctx context.Context, question string, db *pgxpool.Pool, llmClient llm.Client) (string, []string, error) {
	embedding, err := llmClient.GenerateEmbedding(ctx, "search", question)
	if err != nil {
		return "", nil, fmt.Errorf("generating embedding: %w", err)
	}
	vec := pgvector.NewVector(embedding)

	docs, err := schema.New(db).GetClosestDocs(ctx, schema.GetClosestDocsParams{
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

	answer, err := llmClient.GenerateDocumentationResponse(ctx, question, contents)
	if err != nil {
		return "", nil, fmt.Errorf("generating documentation response: %w", err)
	}

	if answer == "" {
		return "", nil, nil
	}

	links := make([]string, 0, len(docs))
	for _, doc := range docs {
		links = append(links, fmt.Sprintf("%s/blob/%s/%s", doc.Url, doc.Revision, doc.Path))
	}

	return answer, links, nil
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
