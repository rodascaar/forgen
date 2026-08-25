package ports

import (
	"context"

	"github.com/rodascaar/forgen/internal/core/domain"
)

// ToolExecutor ejecuta llamadas a herramientas.
type ToolExecutor interface {
	// Execute ejecuta una llamada a herramienta y devuelve el resultado.
	Execute(ctx context.Context, call domain.ToolCall) domain.ToolResult
	// ListTools devuelve las herramientas disponibles para el modelo.
	ListTools() []domain.Tool
	// LookupTools devuelve las herramientas activas con esos nombres.
	LookupTools(names []string) []domain.Tool
}

// PermissionDecider decide si una llamada a herramienta está autorizada.
type PermissionDecider interface {
	// Decide devuelve la decisión sobre una llamada a herramienta.
	Decide(ctx context.Context, sessionID string, call domain.ToolCall) (domain.Decision, error)
}

// PermissionResponder resuelve las llamadas que requieren confirmación del usuario.
type PermissionResponder interface {
	// Confirm pregunta al usuario y devuelve la elección (permitir/denegar/permitir siempre).
	Confirm(ctx context.Context, sessionID string, call domain.ToolCall) (domain.PermissionChoice, error)
	// Remember persiste una regla aprendida de la decisión.
	Remember(ctx context.Context, sessionID string, call domain.ToolCall, level domain.PermissionLevel) error
}

// Messenger comunica eventos del agente a una interfaz (TUI, stdout, JSON).
// La interfaz es un puerto secundario: el dominio solo emite eventos.
type Messenger interface {
	// StreamText entrega un fragmento de texto del modelo.
	StreamText(sessionID string, delta string)
	// ToolStarted notifica el inicio de una herramienta.
	ToolStarted(sessionID string, call domain.ToolCall)
	// ToolFinished notifica el fin de una herramienta.
	ToolFinished(sessionID string, call domain.ToolCall, result domain.ToolResult)
	// Notice muestra un mensaje informativo.
	Notice(sessionID string, text string)
	// Error muestra un error al usuario.
	Error(sessionID string, err error)
	// Finished notifica el fin del turno del agente.
	Finished(sessionID string, finalText string)
}
