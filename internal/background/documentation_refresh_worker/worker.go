package documentation_refresh_worker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"golang.org/x/sync/errgroup"

	"github.com/pgvector/pgvector-go"

	"github.com/dynoinc/ratchet/internal"
	"github.com/dynoinc/ratchet/internal/background"
	"github.com/dynoinc/ratchet/internal/docs"
	"github.com/dynoinc/ratchet/internal/llm"
	dbschema "github.com/dynoinc/ratchet/internal/storage/schema"
)

type documentRefreshWorker struct {
	river.WorkerDefaults[background.DocumentationRefreshArgs]

	bot       *internal.Bot
	llmClient llm.Client
}

func New(bot *internal.Bot, llmClient llm.Client) river.Worker[background.DocumentationRefreshArgs] {
	return &documentRefreshWorker{
		bot:       bot,
		llmClient: llmClient,
	}
}

func (d *documentRefreshWorker) Timeout(job *river.Job[background.DocumentationRefreshArgs]) time.Duration {
	return 15 * time.Minute
}

func (d *documentRefreshWorker) Work(ctx context.Context, job *river.Job[background.DocumentationRefreshArgs]) error {
	url := job.Args.Source.URL()
	status, err := dbschema.New(d.bot.DB).GetOrInsertDocumentationSource(ctx, url)
	if err != nil {
		return fmt.Errorf("getting documentation status for URL %s: %w", url, err)
	}

	it, newRevision, errf := job.Args.Source.ChangesSince(ctx, status.Revision)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(8)
	for update := range it {
		g.Go(func() error {
			if err := processUpdate(gctx, d.bot, d.llmClient, job.Args.Source, update); err != nil {
				slog.Error("Error processing document update",
					"path", update.Path,
					"error", err)
			}

			return nil
		})
	}
	g.Wait()

	if err := errf(); err != nil {
		return fmt.Errorf("getting changes since revision %s: %w", status.Revision, err)
	}

	tx, err := d.bot.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	qtx := dbschema.New(tx)
	if err := qtx.UpdateDocumentationSource(ctx, dbschema.UpdateDocumentationSourceParams{
		Url:      url,
		Revision: newRevision,
	}); err != nil {
		return fmt.Errorf("updating documentation status for URL %s: %w", url, err)
	}

	if _, err = river.JobCompleteTx[*riverpgxv5.Driver](ctx, tx, job); err != nil {
		return fmt.Errorf("completing job: %w", err)
	}

	return tx.Commit(ctx)
}

// tuning parameters for text-embedding-3-small (max ~8 k tokens)
const (
	avgCharsPerToken = 4
	tokensPerChunk   = 1000
	overlapTokens    = 100
)

var (
	chunkSize    = tokensPerChunk * avgCharsPerToken // ≃ 4 000 chars
	chunkOverlap = overlapTokens * avgCharsPerToken  // ≃ 400 chars
)

// stripFrontMatter will remove a leading YAML‑style front‑matter block ("---" … "---")
// and parse its key:value lines into a map.
func stripFrontMatter(s string) (body string, metadata map[string]string) {
	metadata = make(map[string]string)
	if !strings.HasPrefix(s, "---") {
		return s, metadata
	}
	parts := strings.SplitN(s, "---", 3)
	if len(parts) < 3 {
		// malformed or no closing '---'
		return s, metadata
	}
	rawMeta := strings.TrimSpace(parts[1])
	for _, line := range strings.Split(rawMeta, "\n") {
		if kv := strings.SplitN(line, ":", 2); len(kv) == 2 {
			key := strings.ToLower(strings.TrimSpace(kv[0]))
			val := strings.TrimSpace(kv[1])
			metadata[key] = val
		}
	}
	// Preserve the leading newline when returning the body
	return "\n" + strings.TrimPrefix(parts[2], "\n"), metadata
}

// chunkContent splits the given content into slices small enough for
// text-embedding-3-small.  If the file is Markdown (.md), front matter is
// stripped before paragraph-aware splitting. Returns the slices
// plus any parsed front‑matter metadata (empty for non‑.md).
func chunkContent(path, content string) ([]string, map[string]string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	meta := make(map[string]string)
	text := content

	if ext == ".md" {
		text, meta = stripFrontMatter(content)
		return splitMarkdownText(text, chunkSize, chunkOverlap), meta, nil
	}

	return splitText(text, chunkSize, chunkOverlap), meta, nil
}

func splitMarkdownText(text string, size, overlap int) []string {
	sections := markdownSections(text)
	chunks := make([]string, 0, len(sections))
	for _, section := range sections {
		chunks = append(chunks, splitText(section, size, overlap)...)
	}
	return chunks
}

func markdownSections(text string) []string {
	lines := strings.Split(text, "\n")
	sections := make([]string, 0, 1)
	current := make([]string, 0, len(lines))

	for _, line := range lines {
		if isMarkdownHeading(line) {
			if section := strings.TrimSpace(strings.Join(current, "\n")); section != "" {
				sections = append(sections, section)
			}
			current = current[:0]
		}
		current = append(current, line)
	}

	if section := strings.TrimSpace(strings.Join(current, "\n")); section != "" {
		sections = append(sections, section)
	}
	return sections
}

func isMarkdownHeading(line string) bool {
	line = strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(line, "#") {
		return false
	}
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	return level <= 6 && level < len(line) && line[level] == ' '
}

func splitText(text string, size, overlap int) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	if size <= 0 {
		return []string{text}
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= size {
		overlap = size / 2
	}

	runes := []rune(text)
	chunks := make([]string, 0, len(runes)/size+1)
	for start := 0; start < len(runes); {
		end := min(start+size, len(runes))
		if end < len(runes) {
			if split := splitBoundary(string(runes[start:end]), size/2); split > 0 {
				end = start + split
			}
		}

		if chunk := strings.TrimSpace(string(runes[start:end])); chunk != "" {
			chunks = append(chunks, chunk)
		}
		if end == len(runes) {
			break
		}
		next := end - overlap
		if next <= start {
			next = end
		}
		start = next
	}

	return chunks
}

func splitBoundary(text string, minRunes int) int {
	for _, separator := range []string{"\n\n", "\n", ". ", " "} {
		idx := strings.LastIndex(text, separator)
		if idx < 0 || utf8.RuneCountInString(text[:idx]) < minRunes {
			continue
		}
		return utf8.RuneCountInString(text[:idx+len(separator)])
	}
	return 0
}

func processUpdate(ctx context.Context, bot *internal.Bot, llmClient llm.Client, source docs.Source, update docs.Update) error {
	_, err := dbschema.New(bot.DB).UpdateDocumentRevisionIfSHAMatches(ctx, dbschema.UpdateDocumentRevisionIfSHAMatchesParams{
		Url:         source.URL(),
		Path:        update.Path,
		BlobSha:     update.BlobSHA,
		NewRevision: update.Revision,
	})
	if err == nil {
		slog.Debug("Document already exists", "path", update.Path)
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("updating document revision: %w", err)
	}

	content, err := source.Get(ctx, update.Path, update.Revision)
	if err != nil {
		return fmt.Errorf("getting document content: %w", err)
	}

	parts, meta, err := chunkContent(update.Path, content)
	if err != nil {
		return fmt.Errorf("chunking content: %w", err)
	}

	chunks := make([]string, 0, len(parts)+1)
	chunkIndices := make([]int32, 0, len(parts)+1)
	embeddings := make([]*pgvector.Vector, 0, len(parts)+1)

	// Only add metadata as a chunk with embedding if metadata is non-empty
	var startIndex int32 = 0
	if len(meta) > 0 {
		metadataChunk := fmt.Sprintf("Metadata: %v", meta)
		embedding, err := llmClient.GenerateEmbedding(ctx, "documentation", metadataChunk)
		if err != nil {
			return fmt.Errorf("generating embedding for metadata: %w", err)
		}
		vec := pgvector.NewVector(embedding)

		chunks = append(chunks, metadataChunk)
		chunkIndices = append(chunkIndices, 0)
		embeddings = append(embeddings, &vec)
		startIndex = 1
	}

	for i, part := range parts {
		if len(part) == 0 {
			continue
		}

		embedding, err := llmClient.GenerateEmbedding(ctx, "documentation", part)
		if err != nil {
			return fmt.Errorf("generating embedding for chunk %d: %w", i, err)
		}
		vec := pgvector.NewVector(embedding)

		chunks = append(chunks, part)
		chunkIndices = append(chunkIndices, int32(i)+startIndex)
		embeddings = append(embeddings, &vec)
	}

	if len(chunkIndices) == 0 {
		return nil
	}

	slog.DebugContext(ctx, "inserting document with embeddings",
		"URL", source.URL(),
		"Path", update.Path,
		"Revision", update.Revision,
		"NumChunks", len(chunks),
	)
	if err := dbschema.New(bot.DB).InsertDocWithEmbeddings(ctx, dbschema.InsertDocWithEmbeddingsParams{
		Url:          source.URL(),
		Path:         update.Path,
		Revision:     update.Revision,
		BlobSha:      update.BlobSHA,
		Content:      content,
		Chunks:       chunks,
		ChunkIndices: chunkIndices,
		Embeddings:   embeddings,
	}); err != nil {
		return fmt.Errorf("inserting document with embeddings: %w", err)
	}

	return nil
}
