// Package ports define las interfaces que el dominio y los casos de uso
// necesitan. Los adapters implementan estos puertos.
package ports

import (
	"context"

	"github.com/rodascaar/forgen/internal/core/domain"
)

// StreamEvent es un evento emitido durante una respuesta en streaming.
type StreamEvent interface{ isStreamEvent() }

// TextDeltaEvent entrega un fragmento de texto.
type TextDeltaEvent struct{ Text string }

func (TextDeltaEvent) isStreamEvent() {}

// ToolCallEvent entrega una llamada a herramienta completada.
type ToolCallEvent struct{ Call domain.ToolCall }

func (ToolCallEvent) isStreamEvent() {}

// UsageEvent entrega el consumo de tokens al final de la respuesta.
type UsageEvent struct{ Usage domain.Usage }

func (UsageEvent) isStreamEvent() {}

// DoneEvent indica el fin de la respuesta con su motivo.
type DoneEvent struct{ Reason domain.FinishReason }

func (DoneEvent) isStreamEvent() {}

// ErrorEvent reporta un error de streaming (no fatal para la sesión).
type ErrorEvent struct{ Err error }

func (ErrorEvent) isStreamEvent() {}

// ChatRequest es la petición a un proveedor LLM.
type ChatRequest struct {
	Model       domain.Model
	Messages    []domain.Message
	Tools       []domain.Tool
	Temperature float64
	MaxTokens   int
}

// StreamHandler recibe los eventos de streaming del proveedor.
// Devuelve un error para abortar el streaming de forma controlada.
type StreamHandler func(event StreamEvent) error

// LLMProvider es el puerto hacia cualquier proveedor de modelos.
// El dominio no conoce el protocolo específico (OpenAI/Anthropic/...).
type LLMProvider interface {
	// Name identifica al proveedor.
	Name() string
	// StreamChat envía la petición y emite eventos por el handler.
	// Nunca debe retornar nil-error sin emitir un DoneEvent.
	StreamChat(ctx context.Context, request ChatRequest, handler StreamHandler) error
	// ListModels devuelve los IDs de modelos disponibles para el usuario
	// autenticado, consultando el endpoint de listado del proveedor.
	ListModels(ctx context.Context) ([]string, error)
}

// LLMProviderFactory crea proveedores LLM para diferentes configuraciones.
type LLMProviderFactory interface {
	CreateWithKeyResolver(config domain.ProviderConfig, keyResolver func(domain.ProviderConfig) string, overrides map[string]string) (LLMProvider, error)
}
