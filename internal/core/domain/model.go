// Package domain contiene las entidades y value objects del núcleo.
// Este paquete NO importa adapters ni dependencias externas.
package domain

import (
	"errors"
	"fmt"
	"strings"
)

// Role es el rol del hablante en una conversación.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// FinishReason indica por qué terminó una respuesta del modelo.
type FinishReason string

const (
	FinishReasonStop            FinishReason = "stop"
	FinishReasonToolCalls       FinishReason = "tool_calls"
	FinishReasonMaxTokens       FinishReason = "length"
	FinishReasonContentFiltered FinishReason = "content_filter"
)

// Usage resume el consumo de tokens de una llamada al modelo.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Model identifica un modelo LLM dentro de un proveedor.
type Model struct {
	Provider string
	ID       string
	Tier     string // light | standard | heavy (para routing futuro)
	Vision   bool
}

func (m Model) Key() string { return fmt.Sprintf("%s/%s", m.Provider, m.ID) }

// ToolCall es la invocación de una herramienta solicitada por el modelo.
type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// ToolResult es el resultado de ejecutar una herramienta.
type ToolResult struct {
	ToolCallID string
	OK         bool
	Output     string
	Error      error
}

// ResultText devuelve el contenido legible del resultado para enviar al modelo.
func (r ToolResult) ResultText() string {
	if r.OK {
		return r.Output
	}
	if r.Error != nil {
		return fmt.Sprintf("ERROR: %v", r.Error)
	}
	return r.Output
}

// ToolStatus es el estado de vida de una herramienta.
type ToolStatus string

const (
	ToolStatusEnabled  ToolStatus = "enabled"
	ToolStatusDisabled ToolStatus = "disabled"
)

// Tool describe una herramienta disponible para el modelo.
type Tool struct {
	Name        string
	Description string
	Status      ToolStatus
	// Schema es el JSON Schema del argumento de la herramienta (map[string]any
	// porque el JSON Schema no es un tipo de Go nativo y varía por provider).
	Schema map[string]any
}

func (t Tool) Enabled() bool { return t.Status == ToolStatusEnabled }

// IsSystemPrompt verifica si el mensaje es del sistema.
func IsSystemPrompt(role Role) bool { return role == RoleSystem }

// Validation helpers del dominio.

// ValidateModelKey valida el formato provider/modelo.
func ValidateModelKey(key string) error {
	if !strings.Contains(key, "/") {
		return errors.New("model key inválido, formato esperado: provider/model")
	}
	return nil
}
