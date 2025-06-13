package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/openai/openai-go"
	"gopkg.in/yaml.v3"
)

type ToolManager interface {
	GetToolsInfo() ([]openai.ChatCompletionToolParam, map[string]string)
	HandleToolCalls(ctx context.Context, toolToBinMap map[string]string, toolCalls []openai.ChatCompletionMessageToolCall) (map[string]string, error)
}

type toolManager struct {
	tools        []openai.ChatCompletionToolParam
	toolToBinMap map[string]string
}

type ToolFiles struct {
	ToolToBinary map[string]string `yaml:"tools"`
}

type ToolOptions struct {
	Tools      []openai.ChatCompletionToolParam
	BinaryPath string
}

type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

type ToolCallInput struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

func NewToolManager(ctx context.Context, configFile string) (ToolManager, error) {
	// Load tool configuration
	var toolToBinary map[string]string
	if configFile == "" {
		toolToBinary = make(map[string]string)
	} else {
		b, err := os.ReadFile(configFile)
		if err != nil {
			return nil, fmt.Errorf("reading tools config file: %w", err)
		}

		var config ToolFiles
		if err := yaml.Unmarshal(b, &config); err != nil {
			return nil, fmt.Errorf("parsing tools config file: %w", err)
		}
		toolToBinary = config.ToolToBinary
	}

	// Pre-load all tools at startup
	allTools := []openai.ChatCompletionToolParam{}
	toolToBinMap := make(map[string]string)

	for _, bin := range toolToBinary {
		toolsForFile, err := getToolsForSingleFile(ctx, bin)
		if err != nil {
			return nil, fmt.Errorf("getting tools for %s: %w", bin, err)
		}

		for _, tool := range toolsForFile {
			if _, ok := toolToBinMap[tool.Function.Name]; ok {
				return nil, fmt.Errorf("duplicate tool name '%s' found in binary '%s'", tool.Function.Name, bin)
			}
			toolToBinMap[tool.Function.Name] = bin
		}
		allTools = append(allTools, toolsForFile...)
	}

	return &toolManager{
		tools:        allTools,
		toolToBinMap: toolToBinMap,
	}, nil
}

func getToolsForSingleFile(ctx context.Context, binaryPath string) ([]openai.ChatCompletionToolParam, error) {
	input := []ToolCallInput{{
		Name:      "list_all_tools",
		Arguments: map[string]interface{}{},
	}}
	results, err := executeTools(ctx, binaryPath, input)
	if err != nil {
		return nil, fmt.Errorf("getting tools: %w", err)
	}
	if len(results) == 0 {
		return []openai.ChatCompletionToolParam{}, nil
	}

	var toolDefinitions []ToolDefinition
	if err := json.Unmarshal([]byte(results[0]), &toolDefinitions); err != nil {
		return nil, fmt.Errorf("parsing raw tool definitions response: %w", err)
	}
	return formatToolDefinitions(toolDefinitions), nil
}

func executeTools(ctx context.Context, binaryPath string, inputs []ToolCallInput) ([]string, error) {
	toolNames := make([]string, len(inputs))
	for i, input := range inputs {
		toolNames[i] = input.Name
	}
	slog.DebugContext(ctx, "executing tools", "tool_names", toolNames)

	inputsJSON, err := json.Marshal(inputs)
	if err != nil {
		return nil, fmt.Errorf("marshalling input: %w", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, binaryPath)
	cmd.Stdin = bytes.NewReader(inputsJSON)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("running tools: %w", err)
	}

	var rawResultsJSON []json.RawMessage
	if err := json.Unmarshal(stdout.Bytes(), &rawResultsJSON); err != nil {
		return nil, fmt.Errorf("parsing tool results: %w", err)
	}

	var results []string
	for _, raw := range rawResultsJSON {
		results = append(results, string(raw))
	}
	return results, nil
}

func formatToolDefinitions(toolDefinitions []ToolDefinition) []openai.ChatCompletionToolParam {
	var goTools []openai.ChatCompletionToolParam

	for _, toolDefinition := range toolDefinitions {
		goTool := openai.ChatCompletionToolParam{
			Function: openai.FunctionDefinitionParam{
				Name:        toolDefinition.Name,
				Description: openai.String(toolDefinition.Description),
				Parameters:  openai.FunctionParameters(toolDefinition.InputSchema),
			},
		}
		goTools = append(goTools, goTool)
	}
	return goTools
}

func (tm *toolManager) GetToolsInfo() ([]openai.ChatCompletionToolParam, map[string]string) {
	if tm == nil {
		return nil, nil
	}
	return tm.tools, tm.toolToBinMap
}

func (tm *toolManager) HandleToolCalls(
	ctx context.Context,
	toolToBinMap map[string]string,
	toolCalls []openai.ChatCompletionMessageToolCall) (map[string]string, error) {
	if tm == nil {
		return nil, nil
	}

	resultsByCallID := make(map[string]string)

	for _, toolCall := range toolCalls {
		var arguments map[string]interface{}

		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &arguments); err != nil {
			return nil, fmt.Errorf("parsing arguments for tool call %s: %w", toolCall.Function.Name, err)
		}

		input := []ToolCallInput{{
			Name:      toolCall.Function.Name,
			Arguments: arguments,
		}}
		result, err := executeTools(ctx, toolToBinMap[toolCall.Function.Name], input)

		if err != nil {
			return nil, fmt.Errorf("executing tools: %w", err)
		}
		resultsByCallID[toolCall.ID] = result[0]
	}
	return resultsByCallID, nil
}
