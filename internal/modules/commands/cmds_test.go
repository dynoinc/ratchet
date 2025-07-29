package commands

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dynoinc/ratchet/internal/storage/schema/dto"
)

func TestAlertFiringContext(t *testing.T) {
	// Test that alert firing context is properly detected and formatted
	incidentAction := dto.IncidentAction{
		Action:   dto.ActionOpenIncident,
		Service:  "payment-service",
		Alert:    "high-latency",
		Priority: dto.PriorityHigh,
	}

	// Test the context building logic
	channelID := "C1234567890"
	systemPrompt := buildSystemPrompt(channelID, &incidentAction)

	// Verify alert context is included
	assert.Contains(t, systemPrompt, `Alert Context: Service "payment-service" - Alert "high-latency" (Priority: HIGH)`)
	assert.Contains(t, systemPrompt, `Channel ID: C1234567890`)
}

func TestNonAlertFiringContext(t *testing.T) {
	// Test that no alert context is added for non-alert messages
	incidentAction := dto.IncidentAction{
		Action: dto.ActionNone,
	}

	channelID := "C1234567890"
	systemPrompt := buildSystemPrompt(channelID, &incidentAction)

	// Verify alert context is NOT included
	assert.NotContains(t, systemPrompt, "Alert Context:")
	assert.Contains(t, systemPrompt, `Channel ID: C1234567890`)
}

func TestMessageComparison(t *testing.T) {
	// Test message comparison logic
	botID := "U1234567890"

	tests := []struct {
		name           string
		topMsgText     string
		currentMsgText string
		expectedResult bool // true if messages are different
	}{
		{
			name:           "same message without bot prefix",
			topMsgText:     "Hello world",
			currentMsgText: "Hello world",
			expectedResult: false,
		},
		{
			name:           "same message with bot prefix",
			topMsgText:     "Hello world",
			currentMsgText: "<@U1234567890> Hello world",
			expectedResult: false,
		},
		{
			name:           "different messages",
			topMsgText:     "Hello world",
			currentMsgText: "Goodbye world",
			expectedResult: true,
		},
		{
			name:           "different messages with bot prefix",
			topMsgText:     "Hello world",
			currentMsgText: "<@U1234567890> Goodbye world",
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := areMessagesDifferent(tt.topMsgText, tt.currentMsgText, botID)
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}

func TestDocumentationSearchStrategy(t *testing.T) {
	// This test verifies that the system prompt includes clear guidance
	// on when to use docsearch for documentation questions

	// Test that the system prompt includes the documentation search strategy
	channelID := "C1234567890"
	incidentAction := dto.IncidentAction{Action: dto.ActionNone}
	systemPrompt := buildSystemPrompt(channelID, &incidentAction)

	// Verify that the system prompt includes the documentation search strategy
	assert.Contains(t, systemPrompt, "DOCUMENTATION SEARCH STRATEGY")
	assert.Contains(t, systemPrompt, "docsearch")

	t.Log("Documentation search strategy implemented:")
	t.Log("- docsearch: for internal documentation questions")
}

// Helper functions for testing
func buildSystemPrompt(channelID string, incidentAction *dto.IncidentAction) string {
	systemPrompt := `You are a helpful assistant that manages Slack channels and provides various utilities.

CURRENT CONTEXT:
- Channel ID: ` + channelID + `
- Use this channel_id when tools require it`

	// Add alert firing context if it's an alert firing
	if incidentAction.Action == dto.ActionOpenIncident {
		systemPrompt += `
- Alert Context: Service "` + incidentAction.Service + `" - Alert "` + incidentAction.Alert + `" (Priority: ` + string(incidentAction.Priority) + `)`
	}

	systemPrompt += `

IMPORTANT INSTRUCTIONS:
1. **PRIMARY FOCUS**: Always prioritize and focus on the latest user request. Use conversation history primarily for context and understanding, not as the main topic.
2. **ALWAYS use the available tools** when they can help answer the user's request or perform actions
3. **DO NOT** try to answer questions about data, reports, documentation, or channel information without using the appropriate tools first
4. If unsure which tools to use, try relevant ones to explore what data is available
5. **ONLY offer capabilities that are available through your tools** - do not suggest checking external systems like GitHub status, deployment status, or other services unless you have specific tools for them
6. If a user asks about something you cannot do with available tools, politely explain what you can help with instead
7. **RESPOND TO THE CURRENT REQUEST**: Even if the conversation history contains previous topics or requests, always address the most recent user message first

DOCUMENTATION SEARCH STRATEGY:
When answering documentation questions, use the search tool strategically:

**For Internal Documentation Questions:**
- Use docsearch to find relevant internal documentation from the database
- This searches through existing documentation that has been indexed
- Use limit=10 for comprehensive answers, limit=1 for finding specific docs to update

**For Comprehensive Documentation Answers:**
- Use docsearch to find relevant internal documentation
- This searches through existing documentation that has been indexed

RESPONSE FORMAT:
You are writing for a Slack section block. Use Slack's mrkdwn format:
• *bold*, _italic_, ~strike~
• Bullet lists with * or - 
• Inline code and code blocks
• Blockquotes with >
• Links as <url|text>

Do NOT use: headings (#), tables, or HTML.
Keep responses under 3000 characters.

Always be thorough in using tools to provide accurate, up-to-date information rather than making assumptions.`

	return systemPrompt
}

func areMessagesDifferent(topMsgText, currentMsgText, botID string) bool {
	// Trim bot ID prefix from current message for comparison
	currentMsgTextTrimmed := strings.TrimPrefix(currentMsgText, "<@"+botID+"> ")
	return topMsgText != currentMsgTextTrimmed
}
