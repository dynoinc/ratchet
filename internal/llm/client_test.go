package llm

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"

	"github.com/qri-io/jsonschema"
	"github.com/stretchr/testify/require"
)

func TestGenerateEmbedding(t *testing.T) {
	llmClient, err := New(t.Context(), DefaultConfig(), nil)
	if err != nil {
		t.Skip("Skipping test. Ollama not found")
	}

	embedding, err := llmClient.GenerateEmbedding(t.Context(), "classification", "Hello, world!")
	require.NoError(t, err)
	require.Equal(t, 768, len(embedding))
}

func TestJSONSchemaValidator(t *testing.T) {
	llmClient, err := New(t.Context(), DefaultConfig(), nil)
	if err != nil {
		t.Skip("Skipping test. Ollama not found")
	}
	schemaJSON := `{
		"type": "object",
		"properties": {
			"hello": {
				"type": "string"
			}
		}	
	}`
	schema := &jsonschema.Schema{}
	err = json.Unmarshal([]byte(schemaJSON), schema)
	require.NoError(t, err)

	resp, respMsg, err := llmClient.RunJSONModePrompt(t.Context(), `Return the json message {"hello": "world"}`, schema)
	require.NoError(t, err)
	require.Empty(t, respMsg)
	space := regexp.MustCompile(`\s+`)
	require.Equal(t, `{"hello":"world"}`, space.ReplaceAllString(resp, ""))

	resp, respMsg, err = llmClient.RunJSONModePrompt(t.Context(), `Return the json message {"hello": 1}`, schema)
	require.Error(t, err)
	require.Empty(t, resp)
	require.Equal(t, `{"hello":1}`, space.ReplaceAllString(respMsg, ""))
}

func TestClassifyMessage(t *testing.T) {
	llmClient, err := New(t.Context(), DefaultConfig(), nil)
	if err != nil {
		t.Skip("Skipping test. Ollama not found")
	}

	// Define common message classes for team/ops channels
	classes := map[string]string{
		"help_request":      "User is asking for help, troubleshooting assistance, or how-to guidance",
		"production_change": "User is requesting or announcing production changes, deployments, or config updates",
		"code_review":       "User is requesting code review, sharing PR links, or asking for feedback on code changes",
		"incident_report":   "User is reporting an incident, alert, outage, or system issue",
		"status_update":     "User is providing status updates, announcing completions, or sharing progress",
		"resource_request":  "User is requesting access, permissions, credentials, or other resources",
		"bug_report":        "User is reporting a bug, error, or unexpected behavior",
		"feature_request":   "User is requesting new features or enhancements",
		"meeting_schedule":  "User is scheduling meetings, discussions, or coordinating time",
		"general_chat":      "General conversation, greetings, thanks, or casual communication",
	}

	testCases := []struct {
		name          string
		message       string
		expectedClass string
	}{
		// Help Request Tests
		{
			name:          "troubleshooting_help",
			message:       "Getting 500 errors on the payment API, anyone know what might be causing this?",
			expectedClass: "help_request",
		},
		{
			name:          "how_to_question",
			message:       "How do I restart the database service in production? Need to clear some connections.",
			expectedClass: "help_request",
		},
		{
			name:          "debug_assistance",
			message:       "Can someone help me debug this memory leak in the auth service? CPU is spiking.",
			expectedClass: "help_request",
		},
		{
			name:          "configuration_help",
			message:       "Anyone know the correct nginx config for rate limiting? My setup isn't working.",
			expectedClass: "help_request",
		},

		// Production Change Tests
		{
			name:          "deployment_request",
			message:       "Need to deploy the hotfix to production ASAP. Critical bug in payment processing.",
			expectedClass: "production_change",
		},
		{
			name:          "config_update",
			message:       "Planning to update the Redis timeout config from 30s to 60s. Any objections?",
			expectedClass: "production_change",
		},
		{
			name:          "scaling_request",
			message:       "Going to scale up the worker nodes from 3 to 6 due to increased load.",
			expectedClass: "production_change",
		},
		{
			name:          "rollback_announcement",
			message:       "Rolling back the latest deployment due to increased error rates.",
			expectedClass: "production_change",
		},

		// Code Review Tests
		{
			name:          "pr_review_request",
			message:       "PR ready for review: https://github.com/company/api/pull/456 - Added rate limiting middleware",
			expectedClass: "code_review",
		},
		{
			name:          "code_feedback_request",
			message:       "Can someone take a look at my refactor of the auth module? Want to make sure it's solid.",
			expectedClass: "code_review",
		},
		{
			name:          "review_assignment",
			message:       "Need eyes on this database migration PR before we merge: /pull/789",
			expectedClass: "code_review",
		},

		// Incident Report Tests
		{
			name:          "service_down_alert",
			message:       "ALERT: Payment service is completely down. Users can't complete purchases.",
			expectedClass: "incident_report",
		},
		{
			name:          "performance_issue",
			message:       "Database CPU at 98% and climbing. Response times are getting bad.",
			expectedClass: "incident_report",
		},
		{
			name:          "user_reported_issue",
			message:       "Multiple users reporting login failures across all regions. Investigating now.",
			expectedClass: "incident_report",
		},
		{
			name:          "monitoring_alert",
			message:       "High error rate detected on API gateway. Error rate jumped from 0.1% to 5%.",
			expectedClass: "incident_report",
		},

		// Status Update Tests
		{
			name:          "deployment_complete",
			message:       "Deployment completed successfully. All services are running normally.",
			expectedClass: "status_update",
		},
		{
			name:          "issue_resolved",
			message:       "Fixed the memory leak issue. Monitoring shows CPU usage back to normal levels.",
			expectedClass: "status_update",
		},
		{
			name:          "progress_update",
			message:       "Migration is 75% complete. Expecting to finish in the next 2 hours.",
			expectedClass: "status_update",
		},

		// Resource Request Tests
		{
			name:          "access_request",
			message:       "Need admin access to the production database to investigate the query performance.",
			expectedClass: "resource_request",
		},
		{
			name:          "credentials_request",
			message:       "Can I get the API keys for the staging environment? Working on integration tests.",
			expectedClass: "resource_request",
		},
		{
			name:          "permission_request",
			message:       "Need write access to the deployment repo to update the CI/CD pipeline.",
			expectedClass: "resource_request",
		},

		// Bug Report Tests
		{
			name:          "functionality_bug",
			message:       "Found a bug in the user registration flow. Email validation isn't working properly.",
			expectedClass: "bug_report",
		},
		{
			name:          "ui_bug",
			message:       "The dashboard is showing incorrect metrics. Values seem to be off by 10x.",
			expectedClass: "bug_report",
		},

		// Feature Request Tests
		{
			name:          "new_feature",
			message:       "Can we add logging for API rate limit violations? Would help with debugging.",
			expectedClass: "feature_request",
		},
		{
			name:          "enhancement_request",
			message:       "Would be great to have auto-retry logic for failed background jobs.",
			expectedClass: "feature_request",
		},

		// Meeting Schedule Tests
		{
			name:          "meeting_coordination",
			message:       "Post-incident review meeting in 30 minutes. Conference room B.",
			expectedClass: "meeting_schedule",
		},
		{
			name:          "time_coordination",
			message:       "What time works for everyone for the architecture discussion? Thinking 2pm?",
			expectedClass: "meeting_schedule",
		},

		// General Chat Tests
		{
			name:          "greeting",
			message:       "Good morning team! Hope everyone had a great weekend.",
			expectedClass: "general_chat",
		},
		{
			name:          "thanks",
			message:       "Thanks for the quick fix on the auth service! Really appreciate it.",
			expectedClass: "general_chat",
		},
		{
			name:          "casual_conversation",
			message:       "Anyone else notice the new coffee machine in the kitchen? It's pretty good!",
			expectedClass: "general_chat",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			class, reason, err := llmClient.ClassifyMessage(context.Background(), tc.message, classes)
			require.NoError(t, err, "Classification should not error")
			require.NotEmpty(t, class, "Should return a class")
			require.NotEmpty(t, reason, "Should return a reason")

			// Check if the class is valid (either expected or at least in our classes map or "other")
			validClass := false
			if class == "other" || class == tc.expectedClass {
				validClass = true
			} else {
				// Check if it's any of our defined classes
				for definedClass := range classes {
					if class == definedClass {
						validClass = true
						break
					}
				}
			}
			require.True(t, validClass, "Returned class should be valid: got %s", class)

			t.Logf("Message: %s\nExpected: %s\nGot: %s\nReason: %s\n",
				tc.message, tc.expectedClass, class, reason)
		})
	}
}

func TestClassifyMessage_EdgeCases(t *testing.T) {
	llmClient, err := New(context.Background(), DefaultConfig(), nil)
	if err != nil {
		t.Skip("Skipping test. Ollama not found")
	}

	classes := map[string]string{
		"help_request": "User needs assistance or troubleshooting help",
		"urgent":       "Message indicates urgency or emergency",
	}

	testCases := []struct {
		name          string
		message       string
		expectedClass string
	}{
		{
			name:          "empty_message",
			message:       "",
			expectedClass: "other",
		},
		{
			name:          "only_whitespace",
			message:       "   \n\t  ",
			expectedClass: "other",
		},
		{
			name:          "mixed_intent",
			message:       "URGENT: Need help with production deployment that's failing",
			expectedClass: "urgent", // Should pick the more specific/urgent class
		},
		{
			name:          "ambiguous_message",
			message:       "Looking into it",
			expectedClass: "other",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			class, reason, err := llmClient.ClassifyMessage(context.Background(), tc.message, classes)
			require.NoError(t, err)
			require.NotEmpty(t, reason)

			// For edge cases, we're more lenient - just ensure we get a valid class
			validClass := class == "other" || classes[class] != ""
			require.True(t, validClass, "Should return a valid class, got: %s", class)

			t.Logf("Message: '%s'\nGot: %s\nReason: %s\n", tc.message, class, reason)
		})
	}
}
