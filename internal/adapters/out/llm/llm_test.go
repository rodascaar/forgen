package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/forgen/forgen/internal/adapters/out/llm"
	"github.com/forgen/forgen/internal/core/domain"
	"github.com/forgen/forgen/internal/core/ports"
)

func sseServer(t *testing.T, lines []string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/event-stream")
		for _, line := range lines {
			if _, err := writer.Write([]byte(line + "\n")); err != nil {
				return
			}
			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func clientFor(serverURL string) *llm.Client {
	client := llm.NewClient(serverURL, "test-key", nil)
	return client
}

func TestOpenAICompatibleStreamingText(t *testing.T) {
	chunk := map[string]any{
		"choices": []map[string]any{
			{"index": 0, "delta": map[string]any{"content": "hola"}, "finish_reason": nil},
		},
	}
	done := map[string]any{
		"choices": []map[string]any{
			{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"},
		},
		"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 1},
	}
	lines := []string{
		"data: " + toJSON(chunk),
		"data: " + toJSON(done),
		"data: [DONE]",
	}
	provider := llm.NewOpenAICompatible("openai", clientFor(sseServer(t, lines).URL))

	var text string
	var reason domain.FinishReason
	err := provider.StreamChat(context.Background(), ports.ChatRequest{
		Model:       domain.Model{Provider: "openai", ID: "gpt-5"},
		Messages:    []domain.Message{domain.NewTextMessage(domain.RoleUser, "hi")},
		Temperature: 0.2,
		MaxTokens:   100,
	}, func(event ports.StreamEvent) error {
		switch typedEvent := event.(type) {
		case ports.TextDeltaEvent:
			text += typedEvent.Text
		case ports.DoneEvent:
			reason = typedEvent.Reason
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if text != "hola" {
		t.Fatalf("text = %q", text)
	}
	if reason != domain.FinishReasonStop {
		t.Fatalf("reason = %q", reason)
	}
}

func TestOpenAICompatibleToolCalls(t *testing.T) {
	chunk1 := map[string]any{
		"choices": []map[string]any{
			{"index": 0, "delta": map[string]any{
				"tool_calls": []map[string]any{
					{"index": 0, "id": "call_1", "function": map[string]any{"name": "read", "arguments": "{\"path\":"}},
				},
			}},
		},
	}
	chunk2 := map[string]any{
		"choices": []map[string]any{
			{"index": 0, "delta": map[string]any{
				"tool_calls": []map[string]any{
					{"index": 0, "function": map[string]any{"arguments": "\"main.go\"}"}},
				},
			}},
		},
	}
	done := map[string]any{
		"choices": []map[string]any{
			{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"},
		},
	}
	lines := []string{
		"data: " + toJSON(chunk1),
		"data: " + toJSON(chunk2),
		"data: " + toJSON(done),
		"data: [DONE]",
	}
	provider := llm.NewOpenAICompatible("openai", clientFor(sseServer(t, lines).URL))

	var calls []domain.ToolCall
	err := provider.StreamChat(context.Background(), ports.ChatRequest{
		Model:    domain.Model{Provider: "openai", ID: "gpt-5"},
		Messages: []domain.Message{domain.NewTextMessage(domain.RoleUser, "read")},
	}, func(event ports.StreamEvent) error {
		if call, ok := event.(ports.ToolCallEvent); ok {
			calls = append(calls, call.Call)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if calls[0].Name != "read" || calls[0].Arguments["path"] != "main.go" {
		t.Fatalf("call = %+v", calls[0])
	}
}

func toJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}
