package llm

import (
	"testing"

	"github.com/rodascaar/forgen/internal/core/domain"
)

func TestBuildAnthropicMessages(t *testing.T) {
	// tool_use debe ir seguido de tool_result; las tool_use huérfanas se descartan.
	messages := []domain.Message{
		domain.NewTextMessage(domain.RoleSystem, "system prompt"),
		domain.NewTextMessage(domain.RoleUser, "hi"),
		domain.NewAssistantWithToolCalls("", []domain.ToolCall{{ID: "t1", Name: "read", Arguments: map[string]any{"path": "a"}}}),
		domain.NewToolResultMessage("t1", "read", domain.ToolResult{OK: true, Output: "contenido"}),
		// tool_use huérfana (sin tool_result siguiente): debe descartarse.
		domain.NewAssistantWithToolCalls("", []domain.ToolCall{{ID: "t2", Name: "bash", Arguments: map[string]any{"command": "ls"}}}),
	}

	system, built := buildAnthropicMessages(messages)
	if system != "system prompt" {
		t.Fatalf("system = %q", system)
	}
	// user + assistant(tool_use) + tool_result = 3; la tool_use huérfana se descartó.
	if len(built) != 3 {
		t.Fatalf("mensajes Anthropic = %d, want 3", len(built))
	}
	if built[0].Role != "user" {
		t.Fatalf("primer mensaje = %q, want user", built[0].Role)
	}
	// Verificar que la tool_use válida se mantiene.
	if built[1].Role != "assistant" || len(built[1].Content) != 1 || built[1].Content[0].Type != "tool_use" {
		t.Fatalf("segundo mensaje malformado: %+v", built[1])
	}
	// Verificar el tool_result.
	if built[2].Role != "user" || built[2].Content[0].Type != "tool_result" {
		t.Fatalf("tool_result malformado: %+v", built[2])
	}
}

func TestBuildAnthropicMessagesPrependsUser(t *testing.T) {
	messages := []domain.Message{
		domain.NewToolResultMessage("t1", "read", domain.ToolResult{OK: true, Output: "x"}),
	}
	_, built := buildAnthropicMessages(messages)
	if len(built) == 0 || built[0].Role != "user" {
		t.Fatalf("se debe anteponer un mensaje de usuario, got %+v", built)
	}
}
