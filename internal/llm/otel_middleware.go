package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Semantic conventions not in Go OpenTelemetry library yet, because they're not stable yet
// https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/
// https://opentelemetry.io/docs/specs/semconv/gen-ai/openai
const (
	GenAISystemKey                  = attribute.Key("gen_ai.system")
	GenAIOperationNameKey           = attribute.Key("gen_ai.operation.name")
	GenAIRequestModelKey            = attribute.Key("gen_ai.request.model")
	GenAIResponseModelKey           = attribute.Key("gen_ai.response.model")
	GenAIUsageInputTokensKey        = attribute.Key("gen_ai.usage.input_tokens")
	GenAIUsageOutputTokensKey       = attribute.Key("gen_ai.usage.output_tokens")
	GenAIRequestTemperatureKey      = attribute.Key("gen_ai.request.temperature")
	GenAIRequestMaxTokensKey        = attribute.Key("gen_ai.request.max_tokens")
	GenAIRequestTopPKey             = attribute.Key("gen_ai.request.top_p")
	GenAIRequestFrequencyPenaltyKey = attribute.Key("gen_ai.request.frequency_penalty")
	GenAIRequestPresencePenaltyKey  = attribute.Key("gen_ai.request.presence_penalty")
	GenAIRequestEncodingFormatsKey  = attribute.Key("gen_ai.request.encoding_formats")
	GenAIResponseIDKey              = attribute.Key("gen_ai.response.id")
	GenAIResponseFinishReasonKey    = attribute.Key("gen_ai.response.finish_reason")
	GenAIResponseFinishReasonsKey   = attribute.Key("gen_ai.response.finish_reasons")

	// Tool execution attributes
	// https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-spans/#execute-tool-span
	GenAIToolNameKey        = attribute.Key("gen_ai.tool.name")
	GenAIToolCallIDKey      = attribute.Key("gen_ai.tool.call.id")
	GenAIToolDescriptionKey = attribute.Key("gen_ai.tool.description")

	// Span events
	// https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-events
	GenAiSystemMessageKey    = "gen_ai.system.message"
	GenAiUserMessageKey      = "gen_ai.user.message"
	GenAiAssistantMessageKey = "gen_ai.assistant.message"
	GenAiChoiceKey           = "gen_ai.choice"
	GenAiMessageContentKey   = attribute.Key("content")
	GenAiToolCallsKey        = attribute.Key("tool_calls")
)

var (
	// Gen AI attribute values
	GenAISystemOpenAI = GenAISystemKey.String("openai")
)

type Operation string

const (
	OperationChat        Operation = "chat"
	OperationEmbeddings  Operation = "embeddings"
	OperationExecuteTool Operation = "execute_tool"
)

type OtelMiddlewareConfig struct {
	AddEventDetails bool
	SampleRoot      bool
}

func NewOtelMiddleware(tracerProvider trace.TracerProvider, config OtelMiddlewareConfig) option.Middleware {
	tracer := tracerProvider.Tracer("openai.otel.middleware")

	return func(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
		parentSpan := trace.SpanFromContext(req.Context())
		if !config.SampleRoot && (parentSpan == nil || !parentSpan.SpanContext().IsValid()) {
			return next(req)
		}

		operation, err := getOperationFromPath(req.URL.Path)
		excludeGenAi := false
		if err != nil {
			if err == ErrUnsupportedOperation {
				excludeGenAi = true
			} else {
				return next(req)
			}
		}

		var reqBody []byte
		if req.Body != nil && req.Method == "POST" {
			var err error
			reqBody, err = io.ReadAll(req.Body)
			if err == nil {
				req.Body = io.NopCloser(bytes.NewBuffer(reqBody))
			} else {
				return next(req)
			}
		}

		var model RequestSpanHydrator
		if len(reqBody) > 0 {
			model, err = extractRequestModel(operation, reqBody)
			if err != nil {
				if err == ErrModelNotFound {
					excludeGenAi = true
				} else {
					return next(req)
				}
			}
		}

		attributes := []attribute.KeyValue{}
		if operation != "" {
			attributes = append(attributes, GenAIOperationNameKey.String(string(operation)))
		}

		// Add server attributes
		if req.URL.Host != "" {
			attributes = append(attributes, semconv.ServerAddress(req.URL.Hostname()))
			if port := req.URL.Port(); port != "" {
				if portInt, err := strconv.Atoi(port); err == nil {
					attributes = append(attributes, semconv.ServerPort(portInt))
				}
			}
		}

		// Add HTTP attributes
		attributes = append(attributes,
			semconv.HTTPRequestMethodKey.String(req.Method),
			semconv.URLFull(req.URL.String()),
		)

		spanName := ""
		if !excludeGenAi && model != nil {
			// Required Gen AI attributes
			attributes = append(attributes, []attribute.KeyValue{
				GenAISystemOpenAI,
				GenAIRequestModelKey.String(model.ModelName()),
			}...)
			// Add Gen AI request attributes from model
			attributes = append(attributes, model.SpanAttributes()...)
			spanName = fmt.Sprintf("%s %s", operation, model.ModelName())
		} else {
			// For operations that aren't supported by this middleware yet, fallback to only HTTP metadata
			spanName = fmt.Sprintf("%s %s", req.Method, req.URL.Path)
		}

		ctx, span := tracer.Start(req.Context(), spanName, trace.WithAttributes(attributes...), trace.WithSpanKind(trace.SpanKindClient))
		defer span.End()

		// Add span events from model
		if !excludeGenAi && model != nil {
			model.AddSpanEvents(span, config.AddEventDetails)
		}

		// Execute the request
		resp, err := next(req.WithContext(ctx))
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.SetAttributes(semconv.ErrorTypeKey.String(reflect.TypeOf(err).String()))
			return resp, err
		}

		// Hydrate span from response
		hydrateSpanFromResponse(span, resp, operation, config)
		return resp, err
	}
}

var ErrUnsupportedOperation = fmt.Errorf("unsupported operation")
var ErrModelNotFound = fmt.Errorf("model not found")

func getOperationFromPath(path string) (Operation, error) {
	if strings.Contains(path, "/chat/completions") {
		return OperationChat, nil
	} else if strings.Contains(path, "/embeddings") {
		return OperationEmbeddings, nil
	}
	return "", ErrUnsupportedOperation
}

// Extract request model as RequestSpanHydrator
func extractRequestModel(operation Operation, reqBody []byte) (RequestSpanHydrator, error) {
	if len(reqBody) == 0 {
		return nil, ErrModelNotFound
	}
	switch operation {
	case OperationChat:
		var chatParams chatCompletionParams
		if err := json.Unmarshal(reqBody, &chatParams); err != nil {
			return nil, err
		} else {
			return &chatParams, nil
		}
	case OperationEmbeddings:
		var embeddingParams embeddingParams
		if err := json.Unmarshal(reqBody, &embeddingParams); err != nil {
			return nil, err
		} else {
			return &embeddingParams, nil
		}
	}
	return nil, ErrModelNotFound
}

// Interface for extracting span metadata from request models
type RequestSpanHydrator interface {
	ModelName() string
	SpanAttributes() []attribute.KeyValue
	AddSpanEvents(span trace.Span, includeDetails bool)
}

type chatCompletionParams struct {
	openai.ChatCompletionNewParams
}

func (c *chatCompletionParams) ModelName() string {
	return c.Model
}

func (c *chatCompletionParams) SpanAttributes() []attribute.KeyValue {
	attrs := []attribute.KeyValue{}
	if c.Temperature.Valid() {
		attrs = append(attrs, GenAIRequestTemperatureKey.Float64(c.Temperature.Value))
	}
	if c.MaxTokens.Valid() {
		attrs = append(attrs, GenAIRequestMaxTokensKey.Int64(c.MaxTokens.Value))
	}
	if c.TopP.Valid() {
		attrs = append(attrs, GenAIRequestTopPKey.Float64(c.TopP.Value))
	}
	if c.FrequencyPenalty.Valid() {
		attrs = append(attrs, GenAIRequestFrequencyPenaltyKey.Float64(c.FrequencyPenalty.Value))
	}
	if c.PresencePenalty.Valid() {
		attrs = append(attrs, GenAIRequestPresencePenaltyKey.Float64(c.PresencePenalty.Value))
	}
	return attrs
}

func (c *chatCompletionParams) AddSpanEvents(span trace.Span, includeDetails bool) {
	for _, msg := range c.Messages {
		attrs := []attribute.KeyValue{
			GenAISystemOpenAI,
		}
		if msg.OfSystem != nil && msg.OfSystem.Content.OfString.Valid() {
			if includeDetails {
				content := msg.OfSystem.Content.OfString.Value
				attrs = append(attrs, GenAiMessageContentKey.String(content))
			}
			span.AddEvent(GenAiSystemMessageKey, trace.WithAttributes(attrs...))
		} else if msg.OfUser != nil && msg.OfUser.Content.OfString.Valid() {
			if includeDetails {
				content := msg.OfUser.Content.OfString.Value
				attrs = append(attrs, GenAiMessageContentKey.String(content))
			}
			span.AddEvent(GenAiUserMessageKey, trace.WithAttributes(attrs...))
		} else if msg.OfAssistant != nil && msg.OfAssistant.Content.OfString.Valid() {
			if includeDetails {
				content := msg.OfAssistant.Content.OfString.Value
				attrs = append(attrs, GenAiMessageContentKey.String(content))
			}
			if len(msg.OfAssistant.ToolCalls) > 0 {
				toolCallsMap := serializeToolCallParams(msg.OfAssistant.ToolCalls, includeDetails)
				if toolCallsJSON, err := json.Marshal(toolCallsMap); err == nil {
					attrs = append(attrs, GenAiToolCallsKey.String(string(toolCallsJSON)))
				}
			}
			span.AddEvent(GenAiAssistantMessageKey, trace.WithAttributes(attrs...))
		}
	}
}

type embeddingParams struct {
	openai.EmbeddingNewParams
}

func (e *embeddingParams) ModelName() string {
	return e.Model
}

func (e *embeddingParams) SpanAttributes() []attribute.KeyValue {
	attrs := []attribute.KeyValue{}
	attrs = append(attrs, GenAIRequestEncodingFormatsKey.String(string(e.EncodingFormat)))
	return attrs
}

func (e *embeddingParams) AddSpanEvents(span trace.Span, includeDetails bool) {
	// No-op for embeddings
}

func hydrateSpanFromResponse(span trace.Span, resp *http.Response, operation Operation, config OtelMiddlewareConfig) {
	if resp == nil {
		return
	}

	// Add HTTP response attributes
	span.SetAttributes(
		semconv.HTTPResponseStatusCode(resp.StatusCode),
	)

	if resp.StatusCode >= 400 {
		span.SetStatus(codes.Error, "")
		span.SetAttributes(semconv.ErrorTypeKey.String(fmt.Sprintf("%d", resp.StatusCode)))
	}
	if resp.StatusCode >= 300 {
		return
	}

	if resp.Body != nil {
		respBody, err := io.ReadAll(resp.Body)
		if err == nil {
			resp.Body = io.NopCloser(bytes.NewBuffer(respBody))

			responseModel, _ := extractResponseModel(operation, respBody)
			if responseModel != nil {
				for _, attr := range responseModel.SpanAttributes() {
					span.SetAttributes(attr)
				}
				responseModel.AddSpanEvents(span, config.AddEventDetails)
			}
		}
	}
}

// Extract response model as ResponseSpanHydrator
func extractResponseModel(operation Operation, respBody []byte) (ResponseSpanHydrator, error) {
	if len(respBody) == 0 {
		return nil, ErrModelNotFound
	}
	switch operation {
	case OperationChat:
		var chatResp openai.ChatCompletion
		if err := json.Unmarshal(respBody, &chatResp); err != nil {
			return nil, err
		}
		params := &chatCompletionResponseParams{
			ChatCompletion: chatResp,
		}
		return params, nil
	case OperationEmbeddings:
		var embeddingResp openai.CreateEmbeddingResponse
		if err := json.Unmarshal(respBody, &embeddingResp); err != nil {
			return nil, err
		}
		params := &embeddingResponseParams{
			CreateEmbeddingResponse: embeddingResp,
		}
		return params, nil
	}
	return nil, ErrModelNotFound
}

// Interface for extracting span metadata from response models
type ResponseSpanHydrator interface {
	SpanAttributes() []attribute.KeyValue
	AddSpanEvents(span trace.Span, includeDetails bool)
}

type chatCompletionResponseParams struct {
	openai.ChatCompletion
}

func (c *chatCompletionResponseParams) SpanAttributes() []attribute.KeyValue {
	attrs := []attribute.KeyValue{}
	if c.ChatCompletion.Model != "" {
		attrs = append(attrs, GenAIResponseModelKey.String(c.ChatCompletion.Model))
	}
	if c.ChatCompletion.ID != "" {
		attrs = append(attrs, GenAIResponseIDKey.String(c.ChatCompletion.ID))
	}
	if c.ChatCompletion.Usage.PromptTokens > 0 {
		attrs = append(attrs, GenAIUsageInputTokensKey.Int64(c.ChatCompletion.Usage.PromptTokens))
	}
	if c.ChatCompletion.Usage.CompletionTokens > 0 {
		attrs = append(attrs, GenAIUsageOutputTokensKey.Int64(c.ChatCompletion.Usage.CompletionTokens))
	}
	if len(c.ChatCompletion.Choices) > 0 {
		finishReasons := make([]string, 0, len(c.ChatCompletion.Choices))
		for _, choice := range c.ChatCompletion.Choices {
			if choice.FinishReason != "" {
				finishReasons = append(finishReasons, choice.FinishReason)
			}
		}
		if len(finishReasons) > 0 {
			attrs = append(attrs, GenAIResponseFinishReasonsKey.StringSlice(finishReasons))
		}
	}
	return attrs
}

type toolCallFunctionData struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
}

type toolCallData struct {
	ID       string               `json:"id"`
	Type     string               `json:"type"`
	Function toolCallFunctionData `json:"function"`
}

func (c *chatCompletionResponseParams) AddSpanEvents(span trace.Span, includeDetails bool) {
	for _, choice := range c.ChatCompletion.Choices {
		attrs := []attribute.KeyValue{
			GenAISystemOpenAI,
			attribute.Key("index").Int64(choice.Index),
		}
		if choice.FinishReason != "" {
			attrs = append(attrs, GenAIResponseFinishReasonKey.String(choice.FinishReason))
		}
		if includeDetails && choice.Message.Content != "" {
			// Map attribute types not supported by OpenTelemetry yet, so just set content attribute
			attrs = append(attrs, GenAiMessageContentKey.String(choice.Message.Content))
		}

		// Map attribute types not supported by OpenTelemetry yet, so just marshall to JSON string
		if len(choice.Message.ToolCalls) > 0 {
			toolCallsMap := serializeToolCalls(choice.Message.ToolCalls, includeDetails)
			if toolCallsJSON, err := json.Marshal(toolCallsMap); err == nil {
				attrs = append(attrs, GenAiToolCallsKey.String(string(toolCallsJSON)))
			}
		}

		span.AddEvent(GenAiChoiceKey, trace.WithAttributes(attrs...))
	}
}

func serializeToolCallParams(toolCalls []openai.ChatCompletionMessageToolCallParam, includeDetails bool) map[string]toolCallData {
	toolCallsMap := make(map[string]toolCallData)
	for _, toolCall := range toolCalls {
		function := toolCallFunctionData{
			Name: toolCall.Function.Name,
		}
		if includeDetails {
			function.Arguments = toolCall.Function.Arguments
		}
		data := toolCallData{
			ID:       toolCall.ID,
			Type:     string(toolCall.Type),
			Function: function,
		}
		toolCallsMap[toolCall.ID] = data
	}
	return toolCallsMap
}

func serializeToolCalls(toolCalls []openai.ChatCompletionMessageToolCall, includeDetails bool) map[string]toolCallData {
	toolCallsMap := make(map[string]toolCallData)
	for _, toolCall := range toolCalls {
		function := toolCallFunctionData{
			Name: toolCall.Function.Name,
		}
		if includeDetails {
			function.Arguments = toolCall.Function.Arguments
		}
		data := toolCallData{
			ID:       toolCall.ID,
			Type:     string(toolCall.Type),
			Function: function,
		}
		toolCallsMap[toolCall.ID] = data
	}
	return toolCallsMap
}

type embeddingResponseParams struct {
	openai.CreateEmbeddingResponse
}

func (e *embeddingResponseParams) SpanAttributes() []attribute.KeyValue {
	attrs := []attribute.KeyValue{}
	attrs = append(attrs, GenAIUsageInputTokensKey.Int64(e.CreateEmbeddingResponse.Usage.PromptTokens))
	return attrs
}

func (e *embeddingResponseParams) AddSpanEvents(span trace.Span, includeDetails bool) {
	// No-op
}
