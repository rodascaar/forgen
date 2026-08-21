package llm

import (
	"testing"

	"github.com/rodascaar/forgen/internal/core/domain"
)

func benchmarkMessages() []domain.Message {
	messages := make([]domain.Message, 0, 50)
	messages = append(messages, domain.NewTextMessage(domain.RoleSystem, "eres un agente de código"))
	for i := 0; i < 20; i++ {
		messages = append(messages,
			domain.NewTextMessage(domain.RoleUser, "escribe una función que haga X"),
			domain.NewAssistantWithToolCalls("voy a leer", []domain.ToolCall{
				{ID: "c1", Name: "read", Arguments: map[string]any{"path": "a.go"}},
			}),
			domain.NewToolResultMessage("c1", "read", domain.ToolResult{OK: true, Output: "contenido del archivo"}),
		)
	}
	return messages
}

func BenchmarkBuildOpenAIMessages(b *testing.B) {
	messages := benchmarkMessages()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buildOpenAIMessages(messages)
	}
}

func BenchmarkBuildAnthropicMessages(b *testing.B) {
	messages := benchmarkMessages()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = buildAnthropicMessages(messages)
	}
}

func BenchmarkParseToolArguments(b *testing.B) {
	raw := `{"path":"main.go","limit":10,"offset":0}`
	target := map[string]any{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = parseArguments(raw, target)
	}
}
