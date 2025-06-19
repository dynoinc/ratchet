package tools

import (
	"github.com/openai/openai-go"
)

// This file contains tool definitions for the commands module.
// Tools are now handled through the simplified JSON-based approach in cmds.go
// rather than complex OpenAI tool calling structures.

// Definitions returns the list of available tools for OpenAI API
func Definitions() []openai.ChatCompletionToolParam {
	return []openai.ChatCompletionToolParam{
		{
			Function: openai.FunctionDefinitionParam{
				Name:        "generate_weekly_report",
				Description: openai.String("Generate a weekly incident report for a Slack channel"),
				Parameters: openai.FunctionParameters{
					"type":       "object",
					"properties": map[string]interface{}{},
					"required":   []string{},
				},
			},
		},
		{
			Function: openai.FunctionDefinitionParam{
				Name:        "generate_usage_report",
				Description: openai.String("Show bot usage statistics for a channel"),
				Parameters: openai.FunctionParameters{
					"type":       "object",
					"properties": map[string]interface{}{},
					"required":   []string{},
				},
			},
		},
		{
			Function: openai.FunctionDefinitionParam{
				Name:        "enable_auto_doc_reply",
				Description: openai.String("Enable automatic documentation responses in a channel"),
				Parameters: openai.FunctionParameters{
					"type":       "object",
					"properties": map[string]interface{}{},
					"required":   []string{},
				},
			},
		},
		{
			Function: openai.FunctionDefinitionParam{
				Name:        "disable_auto_doc_reply",
				Description: openai.String("Disable automatic documentation responses in a channel"),
				Parameters: openai.FunctionParameters{
					"type":       "object",
					"properties": map[string]interface{}{},
					"required":   []string{},
				},
			},
		},
		{
			Function: openai.FunctionDefinitionParam{
				Name:        "lookup_documentation",
				Description: openai.String("Search and find relevant documentation"),
				Parameters: openai.FunctionParameters{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]string{
							"type":        "string",
							"description": "The search query or question to look up in documentation",
						},
					},
					"required": []string{"query"},
				},
			},
		},
		{
			Function: openai.FunctionDefinitionParam{
				Name:        "update_documentation",
				Description: openai.String("Create pull requests to update documentation"),
				Parameters: openai.FunctionParameters{
					"type": "object",
					"properties": map[string]interface{}{
						"request": map[string]string{
							"type":        "string",
							"description": "The documentation update request description",
						},
					},
					"required": []string{"request"},
				},
			},
		},
	}
}
