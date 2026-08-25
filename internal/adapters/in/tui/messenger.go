package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rodascaar/forgen/internal/core/domain"
)

// tuiMessenger comunica los eventos del agente a la interfaz y responde
// confirmaciones de permiso desde el usuario.
type tuiMessenger struct {
	program *tea.Program
}

func newTUIMessenger(program *tea.Program) *tuiMessenger {
	return &tuiMessenger{program: program}
}

// send es un helper seguro: si el programa es nil (estado inconsistente o run
// que sobrevivió a la TUI), se ignora el mensaje en vez de paniquear.
func (m *tuiMessenger) send(msg tea.Msg) {
	if m.program == nil {
		return
	}
	m.program.Send(msg)
}

// StreamText implementa ports.Messenger.
func (m *tuiMessenger) StreamText(_ string, delta string) {
	m.send(streamDeltaMsg{text: delta})
}

// ToolStarted implementa ports.Messenger.
func (m *tuiMessenger) ToolStarted(_ string, call domain.ToolCall) {
	m.send(toolStartedMsg{call: call})
}

// ToolFinished implementa ports.Messenger.
func (m *tuiMessenger) ToolFinished(_ string, call domain.ToolCall, result domain.ToolResult) {
	m.send(toolFinishedMsg{call: call, result: result})
}

// Notice implementa ports.Messenger.
func (m *tuiMessenger) Notice(_ string, text string) {
	m.send(noticeMsg{text: text})
}

// Error implementa ports.Messenger.
func (m *tuiMessenger) Error(_ string, err error) {
	m.send(errorMsg{err: err})
}

// Finished implementa ports.Messenger.
func (m *tuiMessenger) Finished(_ string, finalText string) {
	m.send(finishedMsg{finalText: finalText})
}

// Confirm implementa ports.PermissionResponder (prompt interactivo Y/N/A = allow always).
func (m *tuiMessenger) Confirm(ctx context.Context, _ string, call domain.ToolCall) (domain.PermissionChoice, error) {
	response := make(chan domain.PermissionChoice, 1)
	m.send(confirmRequestMsg{call: call, response: response})
	select {
	case choice := <-response:
		return choice, nil
	case <-ctx.Done():
		return domain.ChoiceDeny(), ctx.Err()
	}
}

// Remember implementa ports.PermissionResponder.
func (m *tuiMessenger) Remember(_ context.Context, _ string, _ domain.ToolCall, _ domain.PermissionLevel) error {
	return nil
}

// -- mensajes de la interfaz --

type streamDeltaMsg struct{ text string }

type toolStartedMsg struct{ call domain.ToolCall }

type toolFinishedMsg struct {
	call   domain.ToolCall
	result domain.ToolResult
}

type noticeMsg struct{ text string }

type errorMsg struct{ err error }

type finishedMsg struct{ finalText string }

type confirmRequestMsg struct {
	call     domain.ToolCall
	response chan domain.PermissionChoice
}

type runDoneMsg struct {
	err       error
	sessionID string
	modelKey  string
	phase     string
}

func toolCallLabel(call domain.ToolCall) string {
	args := make([]string, 0, len(call.Arguments))
	for key, value := range call.Arguments {
		args = append(args, fmt.Sprintf("%s=%v", key, value))
	}
	summary := strings.Join(args, " ")
	if len(summary) > 60 {
		summary = summary[:60] + "..."
	}
	if summary == "" {
		return call.Name
	}
	return fmt.Sprintf("%s %s", call.Name, summary)
}
