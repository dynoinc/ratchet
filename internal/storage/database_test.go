package storage

import (
	"context"
	"testing"

	"github.com/docker/docker/client"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/dynoinc/ratchet/internal/storage/schema"
	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

func requireDocker(t *testing.T) {
	t.Helper()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skipf("docker not available: %v", err)
	}
	if _, err := cli.Ping(t.Context()); err != nil {
		t.Skipf("docker not available: %v", err)
	}
}

func setupTestDB(t *testing.T) *pgxpool.Pool {
	requireDocker(t)

	ctx := t.Context()
	t.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
	postgresContainer, err := postgres.Run(ctx, postgresImage, postgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() { _ = postgresContainer.Terminate(context.Background()) })

	pool, err := New(ctx, postgresContainer.MustConnectionString(ctx, "sslmode=disable"))
	require.NoError(t, err)
	return pool
}

func TestUpdateReaction(t *testing.T) {
	db := setupTestDB(t)
	ctx := t.Context()

	_, err := schema.New(db).AddChannel(ctx, "C0706000000")
	require.NoError(t, err)

	inserted, err := schema.New(db).AddMessage(ctx, schema.AddMessageParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
		Attrs:     dto.MessageAttrs{},
	})
	require.NoError(t, err)
	require.True(t, inserted)

	err = schema.New(db).UpdateReaction(ctx, schema.UpdateReactionParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
		Reaction:  "thumbsup",
		Count:     1,
	})
	require.NoError(t, err)

	msg, err := schema.New(db).GetMessage(ctx, schema.GetMessageParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
	})
	require.NoError(t, err)
	require.Equal(t, 1, msg.Attrs.Reactions["thumbsup"])

	err = schema.New(db).UpdateReaction(ctx, schema.UpdateReactionParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
		Reaction:  "thumbsup",
		Count:     1,
	})
	require.NoError(t, err)

	msg, err = schema.New(db).GetMessage(ctx, schema.GetMessageParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
	})
	require.NoError(t, err)
	require.Equal(t, 2, msg.Attrs.Reactions["thumbsup"])

	err = schema.New(db).UpdateReaction(ctx, schema.UpdateReactionParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
		Reaction:  "thumbsup",
		Count:     -1,
	})
	require.NoError(t, err)

	msg, err = schema.New(db).GetMessage(ctx, schema.GetMessageParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
	})
	require.NoError(t, err)
	require.Equal(t, 1, msg.Attrs.Reactions["thumbsup"])

	err = schema.New(db).UpdateReaction(ctx, schema.UpdateReactionParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
		Reaction:  "thumbsup",
		Count:     -1,
	})
	require.NoError(t, err)

	msg, err = schema.New(db).GetMessage(ctx, schema.GetMessageParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
	})
	require.NoError(t, err)
	require.Empty(t, msg.Attrs.Reactions)

	err = schema.New(db).UpdateReaction(ctx, schema.UpdateReactionParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
		Reaction:  "thumbsup",
		Count:     -1,
	})
	require.NoError(t, err)

	msg, err = schema.New(db).GetMessage(ctx, schema.GetMessageParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
	})
	require.NoError(t, err)
	require.Empty(t, msg.Attrs.Reactions)
}

func TestInsertDocWithEmbeddings(t *testing.T) {
	db := setupTestDB(t)
	ctx := t.Context()

	// Insert URL
	_, err := schema.New(db).GetOrInsertDocumentationSource(ctx, "https://example.com")
	require.NoError(t, err)

	// Create a vector with 768 dimensions (the size required for text-embedding-3-small)
	embedVector := make([]float32, 768)
	for i := 0; i < 768; i++ {
		embedVector[i] = float32(i % 10) // Fill with some pattern of values
	}
	newVec := pgvector.NewVector(embedVector)

	err = schema.New(db).InsertDocWithEmbeddings(ctx, schema.InsertDocWithEmbeddingsParams{
		Url:          "https://example.com",
		Path:         "path/to/file",
		Revision:     "1",
		BlobSha:      "123",
		ChunkIndices: []int32{0, 1, 2},
		Chunks:       []string{"chunk1", "chunk2", "chunk3"},
		Embeddings:   []*pgvector.Vector{&newVec, &newVec, &newVec},
	})
	require.NoError(t, err)

	// now try updating the doc
	err = schema.New(db).InsertDocWithEmbeddings(ctx, schema.InsertDocWithEmbeddingsParams{
		Url:          "https://example.com",
		Path:         "path/to/file",
		Revision:     "2",
		BlobSha:      "456",
		ChunkIndices: []int32{0, 1, 2},
		Chunks:       []string{"chunk4", "chunk5", "chunk6"},
		Embeddings:   []*pgvector.Vector{&newVec, &newVec, &newVec},
	})
	require.NoError(t, err)

	// now try deleting the doc
	err = schema.New(db).DeleteDoc(ctx, schema.DeleteDocParams{
		Url:  "https://example.com",
		Path: "path/to/file",
	})
	require.NoError(t, err)
}

func TestSearchMessagesHybrid(t *testing.T) {
	db := setupTestDB(t)
	ctx := t.Context()

	// Add a channel
	_, err := schema.New(db).AddChannel(ctx, "C0706000000")
	require.NoError(t, err)

	// Create a vector with 768 dimensions for embedding
	embedVector := make([]float32, 768)
	for i := 0; i < 768; i++ {
		embedVector[i] = float32(i % 10)
	}
	newVec := pgvector.NewVector(embedVector)

	// Add messages with text
	_, err = schema.New(db).AddMessage(ctx, schema.AddMessageParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
		Attrs: dto.MessageAttrs{
			Message: dto.SlackMessage{
				Text: "This is a test message about errors",
				User: "U12345",
			},
		},
	})
	require.NoError(t, err)

	// Add embedding to first message
	err = schema.New(db).UpdateMessageAttrs(ctx, schema.UpdateMessageAttrsParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
		Attrs:     dto.MessageAttrs{},
		Embedding: &newVec,
	})
	require.NoError(t, err)

	_, err = schema.New(db).AddMessage(ctx, schema.AddMessageParams{
		ChannelID: "C0706000000",
		Ts:        "1714358401.000000",
		Attrs: dto.MessageAttrs{
			Message: dto.SlackMessage{
				Text: "Another message about debugging issues",
				User: "U12345",
			},
		},
	})
	require.NoError(t, err)

	// Add embedding to second message
	err = schema.New(db).UpdateMessageAttrs(ctx, schema.UpdateMessageAttrsParams{
		ChannelID: "C0706000000",
		Ts:        "1714358401.000000",
		Attrs:     dto.MessageAttrs{},
		Embedding: &newVec,
	})
	require.NoError(t, err)

	// Add a bot message that should be filtered out
	_, err = schema.New(db).AddMessage(ctx, schema.AddMessageParams{
		ChannelID: "C0706000000",
		Ts:        "1714358402.000000",
		Attrs: dto.MessageAttrs{
			Message: dto.SlackMessage{
				Text: "Bot response about errors",
				User: "BOTUSER123",
			},
		},
	})
	require.NoError(t, err)

	// Add embedding to bot message
	err = schema.New(db).UpdateMessageAttrs(ctx, schema.UpdateMessageAttrsParams{
		ChannelID: "C0706000000",
		Ts:        "1714358402.000000",
		Attrs:     dto.MessageAttrs{},
		Embedding: &newVec,
	})
	require.NoError(t, err)

	// Add a thread reply that should also be searchable
	_, err = schema.New(db).AddThreadMessage(ctx, schema.AddThreadMessageParams{
		ChannelID: "C0706000000",
		ParentTs:  "1714358400.000000", // Reply to first message
		Ts:        "1714358403.000000",
		Attrs: dto.MessageAttrs{
			Message: dto.SlackMessage{
				Text: "Thread reply discussing error handling",
				User: "U67890",
			},
		},
	})
	require.NoError(t, err)

	// Add embedding to thread reply
	err = schema.New(db).UpdateMessageAttrs(ctx, schema.UpdateMessageAttrsParams{
		ChannelID: "C0706000000",
		Ts:        "1714358403.000000",
		Attrs:     dto.MessageAttrs{},
		Embedding: &newVec,
	})
	require.NoError(t, err)

	// Test hybrid search - should now include both parent messages and thread replies
	results, err := schema.New(db).SearchMessagesHybrid(ctx, schema.SearchMessagesHybridParams{
		QueryText:      "errors",
		QueryEmbedding: &newVec,
		ChannelNames:   nil, // Search all channels
		BotID:          "BOTUSER123",
		LimitVal:       10,
	})
	require.NoError(t, err)
	require.Len(t, results, 3) // Should exclude bot message but include parent messages + thread reply

	// Verify that we can identify thread vs parent messages
	var parentMessages, threadReplies int
	for _, result := range results {
		if result.ParentTs == nil {
			parentMessages++
		} else {
			threadReplies++
		}
	}
	require.Equal(t, 2, parentMessages) // Two parent messages
	require.Equal(t, 1, threadReplies)  // One thread reply

	// Test with channel filter
	results, err = schema.New(db).SearchMessagesHybrid(ctx, schema.SearchMessagesHybridParams{
		QueryText:      "errors",
		QueryEmbedding: &newVec,
		ChannelNames:   []string{"nonexistent-channel"},
		BotID:          "BOTUSER123",
		LimitVal:       10,
	})
	require.NoError(t, err)
	require.Len(t, results, 0) // Should find no results in nonexistent channel
}

func TestGetThreadMessages(t *testing.T) {
	db := setupTestDB(t)
	ctx := t.Context()

	// Add a channel
	_, err := schema.New(db).AddChannel(ctx, "C0706000000")
	require.NoError(t, err)

	// Add parent message
	_, err = schema.New(db).AddMessage(ctx, schema.AddMessageParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
		Attrs: dto.MessageAttrs{
			Message: dto.SlackMessage{
				Text: "Parent message",
				User: "U12345",
			},
		},
	})
	require.NoError(t, err)

	// Add thread replies
	_, err = schema.New(db).AddThreadMessage(ctx, schema.AddThreadMessageParams{
		ChannelID: "C0706000000",
		ParentTs:  "1714358400.000000",
		Ts:        "1714358401.000000",
		Attrs: dto.MessageAttrs{
			Message: dto.SlackMessage{
				Text: "First reply",
				User: "U12345",
			},
		},
	})
	require.NoError(t, err)

	_, err = schema.New(db).AddThreadMessage(ctx, schema.AddThreadMessageParams{
		ChannelID: "C0706000000",
		ParentTs:  "1714358400.000000",
		Ts:        "1714358402.000000",
		Attrs: dto.MessageAttrs{
			Message: dto.SlackMessage{
				Text: "Bot reply",
				User: "BOTUSER123",
			},
		},
	})
	require.NoError(t, err)

	// Test getting thread messages (replies only, original behavior)
	results, err := schema.New(db).GetThreadMessages(ctx, schema.GetThreadMessagesParams{
		ChannelID: "C0706000000",
		ParentTs:  "1714358400.000000",
		BotID:     "", // Empty string includes all messages
		LimitVal:  100,
	})
	require.NoError(t, err)
	require.Len(t, results, 2) // 2 replies (not including parent)

	// Verify all messages are replies (should have parent_ts set)
	for _, result := range results {
		require.NotNil(t, result.ParentTs, "All messages should be replies with parent_ts set")
		require.Equal(t, "1714358400.000000", *result.ParentTs)
	}

	// Test excluding bot messages
	results, err = schema.New(db).GetThreadMessages(ctx, schema.GetThreadMessagesParams{
		ChannelID: "C0706000000",
		ParentTs:  "1714358400.000000",
		BotID:     "BOTUSER123", // Exclude bot messages
		LimitVal:  100,
	})
	require.NoError(t, err)
	require.Len(t, results, 1) // 1 reply (bot excluded, parent not included)

	// Test with nonexistent thread
	results, err = schema.New(db).GetThreadMessages(ctx, schema.GetThreadMessagesParams{
		ChannelID: "C0706000000",
		ParentTs:  "9999999999.000000",
		BotID:     "",
		LimitVal:  100,
	})
	require.NoError(t, err)
	require.Len(t, results, 0) // No messages in nonexistent thread
}

func TestGetThreadMessagesWithParent(t *testing.T) {
	db := setupTestDB(t)
	ctx := t.Context()

	// Add a channel
	_, err := schema.New(db).AddChannel(ctx, "C0706000000")
	require.NoError(t, err)

	// Add parent message
	_, err = schema.New(db).AddMessage(ctx, schema.AddMessageParams{
		ChannelID: "C0706000000",
		Ts:        "1714358400.000000",
		Attrs: dto.MessageAttrs{
			Message: dto.SlackMessage{
				Text: "Parent message",
				User: "U12345",
			},
		},
	})
	require.NoError(t, err)

	// Add thread replies
	_, err = schema.New(db).AddThreadMessage(ctx, schema.AddThreadMessageParams{
		ChannelID: "C0706000000",
		ParentTs:  "1714358400.000000",
		Ts:        "1714358401.000000",
		Attrs: dto.MessageAttrs{
			Message: dto.SlackMessage{
				Text: "First reply",
				User: "U12345",
			},
		},
	})
	require.NoError(t, err)

	_, err = schema.New(db).AddThreadMessage(ctx, schema.AddThreadMessageParams{
		ChannelID: "C0706000000",
		ParentTs:  "1714358400.000000",
		Ts:        "1714358402.000000",
		Attrs: dto.MessageAttrs{
			Message: dto.SlackMessage{
				Text: "Bot reply",
				User: "BOTUSER123",
			},
		},
	})
	require.NoError(t, err)

	// Test getting all thread messages including parent
	results, err := schema.New(db).GetThreadMessagesWithParent(ctx, schema.GetThreadMessagesWithParentParams{
		ChannelID: "C0706000000",
		ParentTs:  "1714358400.000000",
		BotID:     "", // Empty string includes all messages
		LimitVal:  100,
	})
	require.NoError(t, err)
	require.Len(t, results, 3) // Parent + 2 replies

	// Verify the first message is the parent (should have parent_ts = nil)
	require.Nil(t, results[0].ParentTs, "First message should be the parent")
	require.Equal(t, "1714358400.000000", results[0].Ts)

	// Verify the remaining messages are replies (should have parent_ts set)
	for i := 1; i < len(results); i++ {
		require.NotNil(t, results[i].ParentTs, "Reply messages should have parent_ts set")
		require.Equal(t, "1714358400.000000", *results[i].ParentTs)
	}

	// Test excluding bot messages
	results, err = schema.New(db).GetThreadMessagesWithParent(ctx, schema.GetThreadMessagesWithParentParams{
		ChannelID: "C0706000000",
		ParentTs:  "1714358400.000000",
		BotID:     "BOTUSER123", // Exclude bot messages
		LimitVal:  100,
	})
	require.NoError(t, err)
	require.Len(t, results, 2) // Parent + 1 reply (bot excluded)

	// Test with nonexistent thread
	results, err = schema.New(db).GetThreadMessagesWithParent(ctx, schema.GetThreadMessagesWithParentParams{
		ChannelID: "C0706000000",
		ParentTs:  "9999999999.000000",
		BotID:     "",
		LimitVal:  100,
	})
	require.NoError(t, err)
	require.Len(t, results, 0) // No messages in nonexistent thread
}
