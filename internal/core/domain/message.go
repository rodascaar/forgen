package domain

import (
	"strings"
	"time"
)

// ContentPart es una parte del contenido de un mensaje.
type ContentPart struct {
	Type string // "text" | "tool_call"
	Text string
	Call *ToolCall
}

// Message es un mensaje de la conversación con el modelo.
type Message struct {
	Role       Role
	Content    []ContentPart
	ToolCallID string
	ToolName   string
	CreatedAt  time.Time
	// CompactedAt marca cuando el contenido tool fue pruneado (no-destructivo).
	// Nil = no compactado. Timestamp permite computed view reversible.
	CompactedAt *time.Time `json:"compacted_at,omitempty"`
	// IsSummary marca el mensaje sintético de resumen post-compaction.
	IsSummary bool `json:"is_summary,omitempty"`
}

// NewTextMessage crea un mensaje de texto puro.
func NewTextMessage(role Role, text string) Message {
	return Message{
		Role:      role,
		Content:   []ContentPart{{Type: "text", Text: text}},
		CreatedAt: time.Now(),
	}
}

// NewAssistantWithToolCalls crea un mensaje de asistente con llamadas a herramientas.
func NewAssistantWithToolCalls(text string, calls []ToolCall) Message {
	content := make([]ContentPart, 0, len(calls)+1)
	if text != "" {
		content = append(content, ContentPart{Type: "text", Text: text})
	}
	for _, call := range calls {
		callCopy := call
		content = append(content, ContentPart{Type: "tool_call", Call: &callCopy})
	}
	return Message{
		Role:      RoleAssistant,
		Content:   content,
		CreatedAt: time.Now(),
	}
}

// NewToolResultMessage crea un mensaje con el resultado de una herramienta.
func NewToolResultMessage(toolCallID, toolName string, result ToolResult) Message {
	return Message{
		Role:       RoleTool,
		ToolCallID: toolCallID,
		ToolName:   toolName,
		Content:    []ContentPart{{Type: "text", Text: result.ResultText()}},
		CreatedAt:  time.Now(),
	}
}

// Text devuelve la concatenación de las partes de texto del mensaje.
func (m Message) Text() string {
	var builder strings.Builder
	for _, part := range m.Content {
		if part.Type == "text" {
			builder.WriteString(part.Text)
		}
	}
	return builder.String()
}

// ToolCalls devuelve las llamadas a herramientas del mensaje.
func (m Message) ToolCalls() []ToolCall {
	calls := make([]ToolCall, 0, len(m.Content))
	for _, part := range m.Content {
		if part.Type == "tool_call" && part.Call != nil {
			calls = append(calls, *part.Call)
		}
	}
	return calls
}

// HasToolCalls indica si el mensaje contiene llamadas a herramientas.
func (m Message) HasToolCalls() bool {
	return len(m.ToolCalls()) > 0
}
