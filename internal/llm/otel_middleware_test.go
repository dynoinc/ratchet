package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// SetupTest consolidates reusable test fixtures for otel middleware tests.
type TestEnv struct {
	SpanRecorder   *tracetest.SpanRecorder
	TracerProvider *trace.TracerProvider
	TestServer     *httptest.Server
	Client         openai.Client
}

func SetupTest(t *testing.T, handler http.HandlerFunc, config OtelMiddlewareConfig, extraMiddleware ...option.Middleware) *TestEnv {
	config.SampleRoot = true
	spanRecorder := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(spanRecorder))
	testServer := httptest.NewServer(handler)
	t.Cleanup(func() {
		spanRecorder.Reset()
		testServer.Close()
	})

	allMiddleware := []option.Middleware{
		NewOtelMiddleware(tp, config),
	}
	allMiddleware = append(allMiddleware, extraMiddleware...)

	clientOptions := []option.RequestOption{
		option.WithBaseURL(testServer.URL),
		option.WithAPIKey("test-key"),
		option.WithMiddleware(allMiddleware...),
	}

	client := openai.NewClient(clientOptions...)
	return &TestEnv{
		SpanRecorder:   spanRecorder,
		TracerProvider: tp,
		TestServer:     testServer,
		Client:         client,
	}
}

func TestOtelMiddleware_ChatCompletion(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := openai.ChatCompletion{
			ID:    "chatcmpl-123",
			Model: "gpt-4",
			Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role:    "assistant",
					Content: "Hello! How can I help you today?",
				},
				FinishReason: "stop",
			}},
			Usage: openai.CompletionUsage{
				PromptTokens:     10,
				CompletionTokens: 9,
				TotalTokens:      19,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})
	env := SetupTest(t, handler, OtelMiddlewareConfig{})
	client := env.Client
	spanRecorder := env.SpanRecorder

	// Create a chat completion request
	ctx := context.Background()
	params := openai.ChatCompletionNewParams{
		Model: "gpt-4",
		Messages: []openai.ChatCompletionMessageParamUnion{
			{
				OfUser: &openai.ChatCompletionUserMessageParam{
					Content: openai.ChatCompletionUserMessageParamContentUnion{
						OfString: param.NewOpt("Hello"),
					},
				},
			},
		},
		Temperature: param.NewOpt(0.7),
		MaxTokens:   param.NewOpt[int64](100),
	}

	// Make the request
	_, err := client.Chat.Completions.New(ctx, params)
	if err != nil {
		t.Fatalf("Failed to make chat completion request: %v", err)
	}

	// Verify the span was created and properly hydrated
	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatal("No spans were recorded")
	}

	span := spans[0]

	// Check span name follows Gen AI convention
	if span.Name() != "chat gpt-4" {
		t.Errorf("Expected span name 'chat gpt-4', got '%s'", span.Name())
	}

	// Check Gen AI attributes
	attrs := span.Attributes()
	expectedAttrs := map[attribute.Key]interface{}{
		GenAISystemKey:            "openai",
		GenAIOperationNameKey:     "chat",
		GenAIRequestModelKey:      "gpt-4",
		GenAIResponseModelKey:     "gpt-4",
		GenAIUsageInputTokensKey:  int64(10),
		GenAIUsageOutputTokensKey: int64(9),
	}

	for expectedKey, expectedValue := range expectedAttrs {
		found := false
		for _, attr := range attrs {
			if attr.Key == expectedKey {
				found = true
				switch expectedValue := expectedValue.(type) {
				case string:
					if attr.Value.AsString() != expectedValue {
						t.Errorf("Expected %s='%v', got '%v'", expectedKey, expectedValue, attr.Value.AsString())
					}
				case int64:
					if attr.Value.AsInt64() != expectedValue {
						t.Errorf("Expected %s=%v, got %v", expectedKey, expectedValue, attr.Value.AsInt64())
					}
				}
				break
			}
		}
		if !found {
			t.Errorf("Missing expected attribute: %s", expectedKey)
		}
	}

	// Check additional attributes
	hasTemperature := false
	hasMaxTokens := false
	hasResponseID := false
	hasFinishReasons := false
	var finishReasons []string

	for _, attr := range attrs {
		switch attr.Key {
		case "gen_ai.request.temperature":
			hasTemperature = true
			if attr.Value.AsFloat64() != 0.7 {
				t.Errorf("Expected temperature=0.7, got %v", attr.Value.AsFloat64())
			}
		case "gen_ai.request.max_tokens":
			hasMaxTokens = true
			if attr.Value.AsInt64() != 100 {
				t.Errorf("Expected max_tokens=100, got %v", attr.Value.AsInt64())
			}
		case "gen_ai.response.id":
			hasResponseID = true
			if attr.Value.AsString() != "chatcmpl-123" {
				t.Errorf("Expected response_id='chatcmpl-123', got '%v'", attr.Value.AsString())
			}
		case "gen_ai.response.finish_reasons":
			hasFinishReasons = true
			finishReasons = attr.Value.AsStringSlice()
		}
	}

	if !hasTemperature {
		t.Error("Missing gen_ai.request.temperature attribute")
	}
	if !hasMaxTokens {
		t.Error("Missing gen_ai.request.max_tokens attribute")
	}
	if !hasResponseID {
		t.Error("Missing gen_ai.response.id attribute")
	}
	if !hasFinishReasons {
		t.Error("Missing gen_ai.response.finish_reasons attribute")
	} else {
		expected := []string{"stop"}
		if len(finishReasons) != len(expected) {
			t.Errorf("Expected finish_reasons length %d, got %d", len(expected), len(finishReasons))
		} else {
			for i, v := range expected {
				if finishReasons[i] != v {
					t.Errorf("Expected finish_reasons[%d]='%s', got '%s'", i, v, finishReasons[i])
				}
			}
		}
	}
}

func TestOtelMiddleware_EmbeddingRequest(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := openai.CreateEmbeddingResponse{
			Data: []openai.Embedding{{
				Embedding: []float64{0.1, 0.2, 0.3},
				Index:     0,
			}},
			Model: "text-embedding-ada-002",
			Usage: openai.CreateEmbeddingResponseUsage{
				PromptTokens: 5,
				TotalTokens:  5,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})
	env := SetupTest(t, handler, OtelMiddlewareConfig{})
	client := env.Client
	spanRecorder := env.SpanRecorder

	// Create an embedding request
	ctx := context.Background()
	params := openai.EmbeddingNewParams{
		Model: "text-embedding-ada-002",
		Input: openai.EmbeddingNewParamsInputUnion{
			OfString: param.NewOpt("Hello world"),
		},
		Dimensions: param.NewOpt[int64](3),
	}

	// Make the request
	_, err := client.Embeddings.New(ctx, params)
	if err != nil {
		t.Fatalf("Failed to make embedding request: %v", err)
	}

	// Verify the span was created and properly hydrated
	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatal("No spans were recorded")
	}

	span := spans[0]

	// Check span name follows Gen AI convention
	if span.Name() != "embeddings text-embedding-ada-002" {
		t.Errorf("Expected span name 'embeddings text-embedding-ada-002', got '%s'", span.Name())
	}

	// Check Gen AI attributes
	attrs := span.Attributes()
	expectedAttrs := map[attribute.Key]interface{}{
		GenAISystemKey:           "openai",
		GenAIOperationNameKey:    "embeddings",
		GenAIRequestModelKey:     "text-embedding-ada-002",
		GenAIUsageInputTokensKey: int64(5),
	}

	for expectedKey, expectedValue := range expectedAttrs {
		found := false
		for _, attr := range attrs {
			if attr.Key == expectedKey {
				found = true
				switch expectedValue := expectedValue.(type) {
				case string:
					if attr.Value.AsString() != expectedValue {
						t.Errorf("Expected %s='%v', got '%v'", expectedKey, expectedValue, attr.Value.AsString())
					}
				case int64:
					if attr.Value.AsInt64() != expectedValue {
						t.Errorf("Expected %s=%v, got %v", expectedKey, expectedValue, attr.Value.AsInt64())
					}
				}
				break
			}
		}
		if !found {
			t.Errorf("Missing expected attribute: %s", expectedKey)
		}
	}
}

func TestGetOperationFromPath(t *testing.T) {
	tests := []struct {
		path     string
		expected Operation
		wantErr  bool
	}{
		{"/v1/chat/completions", OperationChat, false},
		{"/chat/completions", OperationChat, false},
		{"/v1/embeddings", OperationEmbeddings, false},
		{"/embeddings", OperationEmbeddings, false},
		{"/unknown/path", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result, err := getOperationFromPath(tt.path)
			if tt.wantErr {
				if err == nil {
					t.Errorf("getOperationFromPath(%q) expected error, got none", tt.path)
				}
			} else {
				if err != nil {
					t.Errorf("getOperationFromPath(%q) unexpected error: %v", tt.path, err)
				}
				if result != tt.expected {
					t.Errorf("getOperationFromPath(%q) = %q, want %q", tt.path, result, tt.expected)
				}
			}
		})
	}
}

func TestOtelMiddleware_ChatCompletion_EmitsGenAIChoiceEvents(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := openai.ChatCompletion{
			ID:    "chatcmpl-456",
			Model: "gpt-4",
			Choices: []openai.ChatCompletionChoice{
				{
					Index:        0,
					FinishReason: "stop",
					Message: openai.ChatCompletionMessage{
						Role:    "assistant",
						Content: "First response",
					},
				},
				{
					Index:        1,
					FinishReason: "length",
					Message: openai.ChatCompletionMessage{
						Role:    "assistant",
						Content: "Second response",
					},
				},
			},
			Usage: openai.CompletionUsage{
				PromptTokens:     5,
				CompletionTokens: 7,
				TotalTokens:      12,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})
	env := SetupTest(t, handler, OtelMiddlewareConfig{AddEventDetails: true})
	client := env.Client
	spanRecorder := env.SpanRecorder

	// Create a chat completion request
	ctx := context.Background()
	params := openai.ChatCompletionNewParams{
		Model: "gpt-4",
		Messages: []openai.ChatCompletionMessageParamUnion{
			{
				OfUser: &openai.ChatCompletionUserMessageParam{
					Content: openai.ChatCompletionUserMessageParamContentUnion{
						OfString: param.NewOpt("Hi!"),
					},
				},
			},
		},
	}

	// Make the request
	_, err := client.Chat.Completions.New(ctx, params)
	if err != nil {
		t.Fatalf("Failed to make chat completion request: %v", err)
	}

	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatal("No spans were recorded")
	}

	span := spans[0]
	events := span.Events()
	var foundChoice0, foundChoice1 bool
	for _, ev := range events {
		if ev.Name == "gen_ai.choice" {
			var idx int64
			var finishReason, content string
			for _, attr := range ev.Attributes {
				switch attr.Key {
				case "index":
					idx = attr.Value.AsInt64()
				case "gen_ai.response.finish_reason":
					finishReason = attr.Value.AsString()
				case "content":
					content = attr.Value.AsString()
				}
			}
			if idx == 0 && finishReason == "stop" && content == "First response" {
				foundChoice0 = true
			}
			if idx == 1 && finishReason == "length" && content == "Second response" {
				foundChoice1 = true
			}
		}
	}
	if !foundChoice0 {
		t.Error("Did not find gen_ai.choice event for first choice")
	}
	if !foundChoice1 {
		t.Error("Did not find gen_ai.choice event for second choice")
	}
}

func TestOtelMiddleware_ChatCompletion_EmitsToolCallsAttributes(t *testing.T) {
	toolCallParam := openai.ChatCompletionMessageToolCallParam{
		ID:   "call_abc123",
		Type: "function",
		Function: openai.ChatCompletionMessageToolCallFunctionParam{
			Name:      "get_weather",
			Arguments: "{\"location\":\"Paris\"}",
		},
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := openai.ChatCompletion{
			ID:    "chatcmpl-789",
			Model: "gpt-4",
			Choices: []openai.ChatCompletionChoice{{
				Index:        0,
				FinishReason: "tool_calls",
				Message: openai.ChatCompletionMessage{
					Role:    "assistant",
					Content: "",
					ToolCalls: []openai.ChatCompletionMessageToolCall{{
						ID:   toolCallParam.ID,
						Type: toolCallParam.Type,
						Function: openai.ChatCompletionMessageToolCallFunction{
							Name:      toolCallParam.Function.Name,
							Arguments: toolCallParam.Function.Arguments,
						},
					}},
				},
			}},
			Usage: openai.CompletionUsage{
				PromptTokens:     5,
				CompletionTokens: 7,
				TotalTokens:      12,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})
	env := SetupTest(t, handler, OtelMiddlewareConfig{AddEventDetails: true})
	client := env.Client
	spanRecorder := env.SpanRecorder

	// Create a chat completion request
	ctx := context.Background()
	params := openai.ChatCompletionNewParams{
		Model: "gpt-4",
		Messages: []openai.ChatCompletionMessageParamUnion{
			{
				OfUser: &openai.ChatCompletionUserMessageParam{
					Content: openai.ChatCompletionUserMessageParamContentUnion{
						OfString: param.NewOpt("Hi!"),
					},
				},
			},
		},
	}

	// Make the request
	_, err := client.Chat.Completions.New(ctx, params)
	if err != nil {
		t.Fatalf("Failed to make chat completion request: %v", err)
	}

	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatal("No spans were recorded")
	}

	span := spans[0]
	found := false
	// Build expected JSON using the param type
	expectedToolCalls := map[string]toolCallData{
		toolCallParam.ID: {
			ID:   toolCallParam.ID,
			Type: string(toolCallParam.Type),
			Function: toolCallFunctionData{
				Name:      toolCallParam.Function.Name,
				Arguments: toolCallParam.Function.Arguments,
			},
		},
	}
	expectedJSON, _ := json.Marshal(expectedToolCalls)

	for _, ev := range span.Events() {
		if ev.Name == "gen_ai.choice" {
			for _, attr := range ev.Attributes {
				if attr.Key == "tool_calls" && attr.Value.AsString() == string(expectedJSON) {
					found = true
				}
			}
		}
	}

	if !found {
		t.Errorf("Missing or incorrect tool_calls attribute with expected JSON: %s", string(expectedJSON))
	}
}

func TestOtelMiddleware_ErrorFromNext(t *testing.T) {
	errMiddleware := func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		return nil, fmt.Errorf("simulated error")
	}
	env := SetupTest(t, nil, OtelMiddlewareConfig{}, errMiddleware)
	client := env.Client
	spanRecorder := env.SpanRecorder

	ctx := context.Background()
	params := openai.ChatCompletionNewParams{
		Model: "gpt-4",
		Messages: []openai.ChatCompletionMessageParamUnion{
			{
				OfUser: &openai.ChatCompletionUserMessageParam{
					Content: openai.ChatCompletionUserMessageParamContentUnion{
						OfString: param.NewOpt("Hello"),
					},
				},
			},
		},
	}
	_, err := client.Chat.Completions.New(ctx, params)
	if err == nil || err.Error() != "simulated error" {
		t.Fatalf("Expected simulated error, got: %v", err)
	}
	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatal("No spans were recorded")
	}
	span := spans[0]
	if span.Status().Code != codes.Error {
		t.Errorf("Expected span status error, got %v", span.Status().Code)
	}
	foundErrType := false
	for _, attr := range span.Attributes() {
		if attr.Key == semconv.ErrorTypeKey {
			foundErrType = true
			if attr.Value.AsString() != "*errors.errorString" {
				t.Errorf("Expected error type '*errors.errorString', got '%s'", attr.Value.AsString())
			}
		}
	}
	if !foundErrType {
		t.Error("Expected error type attribute on span")
	}

	recorded := false
	for _, event := range span.Events() {
		if event.Name == "exception" {
			recorded = true
			break
		}
	}
	if !recorded {
		t.Error("Expected error to be recorded as an exception event on the span")
	}
}

func TestOtelMiddleware_Handles4xxResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"error":"bad request"}`))
	})
	env := SetupTest(t, handler, OtelMiddlewareConfig{})
	client := env.Client
	spanRecorder := env.SpanRecorder

	ctx := context.Background()
	params := openai.ChatCompletionNewParams{
		Model: "gpt-4",
		Messages: []openai.ChatCompletionMessageParamUnion{
			{
				OfUser: &openai.ChatCompletionUserMessageParam{
					Content: openai.ChatCompletionUserMessageParamContentUnion{
						OfString: param.NewOpt("Hello"),
					},
				},
			},
		},
	}
	_, err := client.Chat.Completions.New(ctx, params)
	if err == nil {
		t.Fatal("Expected error for 400 response, got nil")
	}
	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatal("No spans were recorded")
	}
	span := spans[0]
	if span.Status().Code != codes.Error {
		t.Errorf("Expected span status error, got %v", span.Status().Code)
	}
	foundErrType := false
	for _, attr := range span.Attributes() {
		if attr.Key == semconv.ErrorTypeKey {
			foundErrType = true
			if attr.Value.AsString() != "400" {
				t.Errorf("Expected error type '400', got '%s'", attr.Value.AsString())
			}
		}
	}
	if !foundErrType {
		t.Error("Expected error type attribute on span")
	}
}

func TestOtelMiddleware_Handles5xxResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"error":"internal error"}`))
	})
	env := SetupTest(t, handler, OtelMiddlewareConfig{})
	client := env.Client
	spanRecorder := env.SpanRecorder

	ctx := context.Background()
	params := openai.ChatCompletionNewParams{
		Model: "gpt-4",
		Messages: []openai.ChatCompletionMessageParamUnion{
			{
				OfUser: &openai.ChatCompletionUserMessageParam{
					Content: openai.ChatCompletionUserMessageParamContentUnion{
						OfString: param.NewOpt("Hello"),
					},
				},
			},
		},
	}
	_, err := client.Chat.Completions.New(ctx, params)
	if err == nil {
		t.Fatal("Expected error for 500 response, got nil")
	}
	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatal("No spans were recorded")
	}
	span := spans[0]
	if span.Status().Code != codes.Error {
		t.Errorf("Expected span status error, got %v", span.Status().Code)
	}
	foundErrType := false
	for _, attr := range span.Attributes() {
		if attr.Key == semconv.ErrorTypeKey {
			foundErrType = true
			if attr.Value.AsString() != "500" {
				t.Errorf("Expected error type '500', got '%s'", attr.Value.AsString())
			}
		}
	}
	if !foundErrType {
		t.Error("Expected error type attribute on span")
	}
}

func TestOtelMiddleware_FineTuningJob_NoGenAIAttributes(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"id":            "ftjob-123",
			"object":        "fine_tuning.job",
			"model":         "gpt-3.5-turbo",
			"status":        "queued",
			"created_at":    1234567890,
			"training_file": "file-abc123",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})
	env := SetupTest(t, handler, OtelMiddlewareConfig{})
	client := env.Client
	spanRecorder := env.SpanRecorder

	ctx := context.Background()
	params := openai.FineTuningJobNewParams{
		Model:        "gpt-3.5-turbo",
		TrainingFile: "file-abc123",
	}

	_, err := client.FineTuning.Jobs.New(ctx, params)
	if err != nil {
		t.Fatalf("Failed to create finetuning job: %v", err)
	}

	spans := spanRecorder.Ended()
	if len(spans) == 0 {
		t.Fatal("No spans were recorded")
	}

	span := spans[0]

	// The span name should be the fallback (not a GenAI operation)
	expectedSpanName := "POST /fine_tuning/jobs"
	if span.Name() != expectedSpanName {
		t.Errorf("Expected span name '%s', got '%s'", expectedSpanName, span.Name())
	}

	attrs := span.Attributes()
	// Should not have any gen_ai.* attributes
	for _, attr := range attrs {
		if strings.HasPrefix(string(attr.Key), "gen_ai.") {
			t.Errorf("Unexpected GenAI attribute on span: %s", attr.Key)
		}
	}

	// Should have HTTP and server attributes
	hasMethod := false
	hasURL := false
	hasServer := false
	for _, attr := range attrs {
		if attr.Key == semconv.HTTPRequestMethodKey {
			hasMethod = true
		}
		if attr.Key == semconv.URLFullKey {
			hasURL = true
		}
		if attr.Key == semconv.ServerAddressKey {
			hasServer = true
		}
	}
	if !hasMethod {
		t.Error("Missing HTTP method attribute on span")
	}
	if !hasURL {
		t.Error("Missing URL attribute on span")
	}
	if !hasServer {
		t.Error("Missing server address attribute on span")
	}
}
