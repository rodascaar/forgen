package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/forgen/forgen/internal/core/domain"
	"github.com/forgen/forgen/internal/core/ports"
)

// anthropicAPIVersion es la versión del protocolo Messages usada.
const anthropicAPIVersion = "2023-06-01"

// wire types del protocolo Anthropic Messages.

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Tools       []anthropicTool    `json:"tools,omitempty"`
	Temperature float64            `json:"temperature"`
	Stream      bool               `json:"stream"`
}

type anthropicMessage struct {
	Role    string                  `json:"role"`
	Content []anthropicContentBlock `json:"content"`
}

type anthropicContentBlock struct {
	Type      string         `json:"type"` // text | tool_use | tool_result
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   any            `json:"content,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicSSEEvent struct {
	Type         string                 `json:"type"`
	Index        int                    `json:"index,omitempty"`
	ContentBlock *anthropicContentBlock `json:"content_block,omitempty"`
	Delta        *anthropicSSEDelta     `json:"delta,omitempty"`
	Usage        *anthropicUsage        `json:"usage,omitempty"`
}

type anthropicSSEDelta struct {
	Type        string `json:"type"` // text_delta | input_json_delta
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	StopReason  string `json:"stop_reason,omitempty"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Anthropic es el adapter del protocolo Messages de Anthropic.
type Anthropic struct {
	name   string
	client *Client
}

// NewAnthropic construye el adapter de Anthropic.
func NewAnthropic(name string, client *Client) *Anthropic {
	client.ExtraHeaders["anthropic-version"] = anthropicAPIVersion
	return &Anthropic{name: name, client: client}
}

// Name implementa ports.LLMProvider.
func (a *Anthropic) Name() string { return a.name }

// StreamChat implementa ports.LLMProvider.
func (a *Anthropic) StreamChat(ctx context.Context, request ports.ChatRequest, handler ports.StreamHandler) error {
	system, messages := buildAnthropicMessages(request.Messages)
	payload := anthropicRequest{
		Model:       request.Model.ID,
		MaxTokens:   request.MaxTokens,
		System:      system,
		Messages:    messages,
		Tools:       buildAnthropicTools(request.Tools),
		Temperature: request.Temperature,
		Stream:      true,
	}

	response, err := a.client.Do(ctx, "POST", "/v1/messages", func() ([]byte, error) {
		return json.Marshal(payload)
	})
	if err != nil {
		return err
	}
	defer response.Body.Close()

	accumulator := newAnthropicAccumulator(handler, slog.Default())
	if err := a.client.StreamSSE(response.Body, func(data string) error {
		var event anthropicSSEEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return fmt.Errorf("parsear evento SSE Anthropic: %w", err)
		}
		return accumulator.process(event)
	}); err != nil {
		return err
	}
	return accumulator.finish()
}

func buildAnthropicTools(tools []domain.Tool) []anthropicTool {
	result := make([]anthropicTool, 0, len(tools))
	for _, tool := range tools {
		if !tool.Enabled() {
			continue
		}
		schema := tool.Schema
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		result = append(result, anthropicTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: schema,
		})
	}
	return result
}

// buildAnthropicMessages extrae el system prompt y sanea los mensajes
// para cumplir las reglas de alternancia de Anthropic.
func buildAnthropicMessages(messages []domain.Message) (string, []anthropicMessage) {
	system := ""
	filtered := make([]domain.Message, 0, len(messages))
	for _, message := range messages {
		if message.Role == domain.RoleSystem {
			system += message.Text() + "\n"
			continue
		}
		filtered = append(filtered, message)
	}

	result := make([]anthropicMessage, 0, len(filtered))
	for index, message := range filtered {
		switch message.Role {
		case domain.RoleUser:
			content := make([]anthropicContentBlock, 0, 1)
			if text := message.Text(); text != "" {
				content = append(content, anthropicContentBlock{Type: "text", Text: text})
			}
			if len(content) == 0 {
				continue
			}
			result = append(result, anthropicMessage{Role: "user", Content: content})
		case domain.RoleAssistant:
			content := make([]anthropicContentBlock, 0, 2)
			if text := message.Text(); text != "" {
				content = append(content, anthropicContentBlock{Type: "text", Text: text})
			}
			calls := message.ToolCalls()
			if len(calls) > 0 {
				// El siguiente mensaje debe ser un tool_result; si no lo es
				// (sesión cortada a mitad), descartar las tool_use.
				hasResult := index+1 < len(filtered) && filtered[index+1].Role == domain.RoleTool
				if !hasResult {
					calls = nil
				}
			}
			for _, call := range calls {
				input := call.Arguments
				if input == nil {
					input = map[string]any{}
				}
				content = append(content, anthropicContentBlock{
					Type:  "tool_use",
					ID:    call.ID,
					Name:  call.Name,
					Input: input,
				})
			}
			if len(content) == 0 {
				continue
			}
			result = append(result, anthropicMessage{Role: "assistant", Content: content})
		case domain.RoleTool:
			content := []anthropicContentBlock{{
				Type:      "tool_result",
				ToolUseID: message.ToolCallID,
				Content:   message.Text(),
			}}
			result = append(result, anthropicMessage{Role: "user", Content: content})
		}
	}

	// Anthropic exige que la conversación comience con un mensaje de usuario.
	if len(result) > 0 && result[0].Role != "user" {
		result = append([]anthropicMessage{{
			Role:    "user",
			Content: []anthropicContentBlock{{Type: "text", Text: "."}},
		}}, result...)
	}
	return strings.TrimSpace(system), result
}

// anthropicAccumulator ensambla texto y tool_use desde los eventos SSE.
type anthropicAccumulator struct {
	handler  ports.StreamHandler
	pending  map[int]*pendingCall
	order    []int
	stopSeen bool
	logger   *slog.Logger
}

func newAnthropicAccumulator(handler ports.StreamHandler, logger *slog.Logger) *anthropicAccumulator {
	return &anthropicAccumulator{
		handler: handler,
		pending: make(map[int]*pendingCall),
		logger:  logger,
	}
}

func (a *anthropicAccumulator) process(event anthropicSSEEvent) error {
	switch event.Type {
	case "content_block_start":
		if event.ContentBlock != nil && event.ContentBlock.Type == "tool_use" {
			pending := &pendingCall{call: &domain.ToolCall{
				ID:        event.ContentBlock.ID,
				Name:      event.ContentBlock.Name,
				Arguments: map[string]any{},
			}}
			a.pending[event.Index] = pending
			a.order = append(a.order, event.Index)
		}
	case "content_block_delta":
		if event.Delta == nil {
			return nil
		}
		switch event.Delta.Type {
		case "text_delta":
			return a.handler(ports.TextDeltaEvent{Text: event.Delta.Text})
		case "input_json_delta":
			if pending, ok := a.pending[event.Index]; ok {
				pending.arguments += event.Delta.PartialJSON
			}
		}
	case "message_delta":
		if event.Delta != nil && event.Delta.StopReason != "" {
			a.stopSeen = event.Delta.StopReason == "max_tokens"
		}
	case "message_start":
		if event.Usage != nil {
			a.handler(ports.UsageEvent{Usage: domain.Usage{InputTokens: event.Usage.InputTokens}})
		}
	}
	return nil
}

func (a *anthropicAccumulator) finish() error {
	for _, index := range a.order {
		pending := a.pending[index]
		if pending == nil || pending.call.Name == "" {
			a.logger.Warn("anthropic.tool_call_incompleto")
			continue
		}
		if err := parseArguments(pending.arguments, pending.call.Arguments); err != nil {
			a.logger.Warn("anthropic.tool_call_args_inválidos", "err", err)
		}
		if err := a.handler(ports.ToolCallEvent{Call: *pending.call}); err != nil {
			return err
		}
	}
	reason := domain.FinishReasonStop
	if a.stopSeen {
		reason = domain.FinishReasonMaxTokens
	}
	return a.handler(ports.DoneEvent{Reason: reason})
}

var _ ports.LLMProvider = (*Anthropic)(nil)
