package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// wire types del protocolo OpenAI Chat Completions (compatible).

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type openAIRequest struct {
	Model         string          `json:"model"`
	Messages      []openAIMessage `json:"messages"`
	Tools         []openAITool    `json:"tools,omitempty"`
	Temperature   float64         `json:"temperature"`
	MaxTokens     int             `json:"max_tokens"`
	Stream        bool            `json:"stream"`
	StreamOptions *streamOptions  `json:"stream_options,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIChunk struct {
	Choices []openAIChunkChoice `json:"choices"`
	Usage   *openAIUsage        `json:"usage,omitempty"`
}

type openAIChunkChoice struct {
	Delta        openAIChunkDelta `json:"delta"`
	FinishReason *string          `json:"finish_reason"`
}

type openAIChunkDelta struct {
	Content   *string           `json:"content,omitempty"`
	ToolCalls []openAIToolDelta `json:"tool_calls,omitempty"`
}

type openAIToolDelta struct {
	Index    int              `json:"index"`
	ID       *string          `json:"id,omitempty"`
	Function *openAIFuncDelta `json:"function,omitempty"`
}

type openAIFuncDelta struct {
	Name      *string `json:"name,omitempty"`
	Arguments *string `json:"arguments,omitempty"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// OpenAICompatible es el adapter para cualquier API OpenAI-compatible.
type OpenAICompatible struct {
	name   string
	client *Client
}

// NewOpenAICompatible construye el adapter.
func NewOpenAICompatible(name string, client *Client) *OpenAICompatible {
	return &OpenAICompatible{name: name, client: client}
}

// Name implementa ports.LLMProvider.
func (o *OpenAICompatible) Name() string { return o.name }

// StreamChat implementa ports.LLMProvider.
func (o *OpenAICompatible) StreamChat(ctx context.Context, request ports.ChatRequest, handler ports.StreamHandler) error {
	payload := openAIRequest{
		Model:         request.Model.ID,
		Messages:      buildOpenAIMessages(request.Messages),
		Tools:         buildOpenAITools(request.Tools),
		Temperature:   request.Temperature,
		MaxTokens:     request.MaxTokens,
		Stream:        true,
		StreamOptions: &streamOptions{IncludeUsage: true},
	}

	response, err := postJSON(ctx, o.client, "/chat/completions", payload)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()

	accumulator := newOpenAIToolAccumulator(request.Model, handler)

	err = o.client.StreamSSE(response.Body, func(data string) error {
		var chunk openAIChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("parsear chunk SSE: %w", err)
		}
		return accumulator.process(chunk)
	})
	if err != nil {
		return err
	}
	return accumulator.finish()
}

func buildOpenAIMessages(messages []domain.Message) []openAIMessage {
	result := make([]openAIMessage, 0, len(messages))
	for _, message := range messages {
		switch message.Role {
		case domain.RoleTool:
			result = append(result, openAIMessage{
				Role:       "tool",
				ToolCallID: message.ToolCallID,
				Content:    message.Text(),
			})
		case domain.RoleAssistant:
			openAIMessage := openAIMessage{Role: "assistant"}
			if text := message.Text(); text != "" {
				openAIMessage.Content = text
			}
			calls := message.ToolCalls()
			if len(calls) > 0 {
				openAIMessage.Content = nil
				openAIMessage.ToolCalls = make([]openAIToolCall, 0, len(calls))
				for _, call := range calls {
					arguments, _ := json.Marshal(call.Arguments)
					openAIMessage.ToolCalls = append(openAIMessage.ToolCalls, openAIToolCall{
						ID:   call.ID,
						Type: "function",
						Function: openAICallFunction{
							Name:      call.Name,
							Arguments: string(arguments),
						},
					})
				}
			}
			result = append(result, openAIMessage)
		default:
			role := string(message.Role)
			if role == "system" {
				role = "system"
			} else {
				role = "user"
			}
			result = append(result, openAIMessage{Role: role, Content: message.Text()})
		}
	}
	return result
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAICallFunction `json:"function"`
}

type openAICallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func buildOpenAITools(tools []domain.Tool) []openAITool {
	result := make([]openAITool, 0, len(tools))
	for _, tool := range tools {
		if !tool.Enabled() {
			continue
		}
		schema := tool.Schema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		result = append(result, openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  schema,
			},
		})
	}
	return result
}

// pendingCall acumula una llamada en construcción desde deltas SSE.
type pendingCall struct {
	call      *domain.ToolCall
	arguments string
}

// openAIToolAccumulator ensambla las llamadas a herramientas desde deltas.
type openAIToolAccumulator struct {
	model      domain.Model
	handler    ports.StreamHandler
	toolCalls  map[int]*pendingCall
	order      []int
	finishSeen bool
	logger     *slog.Logger
}

func newOpenAIToolAccumulator(model domain.Model, handler ports.StreamHandler) *openAIToolAccumulator {
	return &openAIToolAccumulator{
		model:     model,
		handler:   handler,
		toolCalls: make(map[int]*pendingCall),
		logger:    slog.Default(),
	}
}

func (a *openAIToolAccumulator) process(chunk openAIChunk) error {
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != nil {
			if err := a.handler(ports.TextDeltaEvent{Text: *choice.Delta.Content}); err != nil {
				return err
			}
		}
		for _, delta := range choice.Delta.ToolCalls {
			pending, ok := a.toolCalls[delta.Index]
			if !ok {
				pending = &pendingCall{call: &domain.ToolCall{Arguments: map[string]any{}}}
				a.toolCalls[delta.Index] = pending
				a.order = append(a.order, delta.Index)
			}
			if delta.ID != nil {
				pending.call.ID = *delta.ID
			}
			if pending.call.ID == "" {
				pending.call.ID = fmt.Sprintf("call_%d", delta.Index)
			}
			if delta.Function != nil {
				if delta.Function.Name != nil {
					pending.call.Name = *delta.Function.Name
				}
				if delta.Function.Arguments != nil {
					pending.arguments += *delta.Function.Arguments
				}
			}
		}
		if choice.FinishReason != nil && *choice.FinishReason == "tool_calls" {
			a.finishSeen = true
		}
	}
	if chunk.Usage != nil {
		if err := a.handler(ports.UsageEvent{Usage: domain.Usage{
			InputTokens:  chunk.Usage.PromptTokens,
			OutputTokens: chunk.Usage.CompletionTokens,
		}}); err != nil {
			return err
		}
	}
	return nil
}

func (a *openAIToolAccumulator) finish() error {
	for _, index := range a.order {
		pending := a.toolCalls[index]
		if pending.call.Name == "" {
			a.logger.Warn("llm.tool_call_incompleto", "model", a.model.Key())
			continue
		}
		if err := parseArguments(pending.arguments, pending.call.Arguments); err != nil {
			a.logger.Warn("llm.tool_call_args_inválidos", "model", a.model.Key(), "err", err)
		}
		if err := a.handler(ports.ToolCallEvent{Call: *pending.call}); err != nil {
			return err
		}
	}
	reason := domain.FinishReasonStop
	if a.finishSeen {
		reason = domain.FinishReasonToolCalls
	}
	return a.handler(ports.DoneEvent{Reason: reason})
}

var _ ports.LLMProvider = (*OpenAICompatible)(nil)
