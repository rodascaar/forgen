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

// StreamText implementa ports.Messenger.
func (m *tuiMessenger) StreamText(_ string, delta string) {
	m.program.Send(streamDeltaMsg{text: delta})
}

// ToolStarted implementa ports.Messenger.
func (m *tuiMessenger) ToolStarted(_ string, call domain.ToolCall) {
	m.program.Send(toolStartedMsg{call: call})
}

// ToolFinished implementa ports.Messenger.
func (m *tuiMessenger) ToolFinished(_ string, call domain.ToolCall, result domain.ToolResult) {
	m.program.Send(toolFinishedMsg{call: call, result: result})
}

// Notice implementa ports.Messenger.
func (m *tuiMessenger) Notice(_ string, text string) {
	m.program.Send(noticeMsg{text: text})
}

// Error implementa ports.Messenger.
func (m *tuiMessenger) Error(_ string, err error) {
	m.program.Send(errorMsg{err: err})
}

// Finished implementa ports.Messenger.
func (m *tuiMessenger) Finished(_ string, finalText string) {
	m.program.Send(finishedMsg{finalText: finalText})
}

// Confirm implementa ports.PermissionResponder (prompt interactivo Y/N).
func (m *tuiMessenger) Confirm(ctx context.Context, _ string, call domain.ToolCall) (bool, error) {
	response := make(chan bool, 1)
	m.program.Send(confirmRequestMsg{call: call, response: response})
	select {
	case allowed := <-response:
		return allowed, nil
	case <-ctx.Done():
		return false, ctx.Err()
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
	response chan bool
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
