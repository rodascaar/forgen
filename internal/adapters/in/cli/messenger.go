package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/forgen/forgen/internal/core/domain"
)

// textMessenger imprime el flujo del agente en texto plano (headless).
type textMessenger struct {
	out  io.Writer
	in   io.Reader
	json bool
}

func newTextMessenger(out io.Writer, in io.Reader) *textMessenger {
	return &textMessenger{out: out, in: in}
}

func newJSONMessenger(out io.Writer, in io.Reader) *textMessenger {
	return &textMessenger{out: out, in: in, json: true}
}

// StreamText implementa ports.Messenger.
func (m *textMessenger) StreamText(_ string, delta string) {
	if m.json {
		m.emit(streamTextEvent{Type: "stream_text", Text: delta})
		return
	}
	_, _ = fmt.Fprint(m.out, delta)
}

// ToolStarted implementa ports.Messenger.
func (m *textMessenger) ToolStarted(_ string, call domain.ToolCall) {
	if m.json {
		m.emit(toolStartedEvent{Type: "tool_started", Tool: call.Name, Arguments: call.Arguments})
		return
	}
	_, _ = fmt.Fprintf(m.out, "\n\x1b[36m▶ %s\x1b[0m\n", toolCallSummary(call))
}

// ToolFinished implementa ports.Messenger.
func (m *textMessenger) ToolFinished(_ string, call domain.ToolCall, result domain.ToolResult) {
	if m.json {
		m.emit(toolFinishedEvent{Type: "tool_finished", Tool: call.Name, OK: result.OK, Output: summarize(result.Output)})
		return
	}
	if result.OK {
		_, _ = fmt.Fprintf(m.out, "\x1b[32m✓ %s\x1b[0m\n", summarize(result.Output))
	} else {
		_, _ = fmt.Fprintf(m.out, "\x1b[31m✗ %s: %v\x1b[0m\n", call.Name, result.Error)
	}
}

// Notice implementa ports.Messenger.
func (m *textMessenger) Notice(_ string, text string) {
	if m.json {
		m.emit(noticeEvent{Type: "notice", Text: text})
		return
	}
	_, _ = fmt.Fprintf(m.out, "\n\x1b[33m⚠ %s\x1b[0m\n", text)
}

// Error implementa ports.Messenger.
func (m *textMessenger) Error(_ string, err error) {
	if m.json {
		m.emit(errorEvent{Type: "error", Error: err.Error()})
		return
	}
	_, _ = fmt.Fprintf(m.out, "\n\x1b[31mError: %v\x1b[0m\n", err)
}

// Finished implementa ports.Messenger.
func (m *textMessenger) Finished(_ string, finalText string) {
	if m.json {
		m.emit(finishedEvent{Type: "finished", Text: finalText})
	}
	// En modo texto el texto final ya se emitió por StreamText.
}

// Confirm implementa ports.PermissionResponder (prompt Y/N en terminal).
func (m *textMessenger) Confirm(_ context.Context, _ string, call domain.ToolCall) (bool, error) {
	if !m.json {
		_, _ = fmt.Fprintf(m.out, "\n\x1b[33mPermiso para: %s\x1b[0m\n", toolCallSummary(call))
		_, _ = fmt.Fprint(m.out, "¿Permitir? [y/N] ")
	}
	scanner := bufio.NewScanner(m.in)
	if !scanner.Scan() {
		return false, fmt.Errorf("sin entrada del usuario")
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes", nil
}

// Remember implementa ports.PermissionResponder.
func (m *textMessenger) Remember(_ context.Context, _ string, _ domain.ToolCall, _ domain.PermissionLevel) error {
	// En headless las reglas persistentes se gestionan vía forgen config.
	return nil
}

// -- eventos JSON del modo --json --

type streamTextEvent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolStartedEvent struct {
	Type      string         `json:"type"`
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
}

type toolFinishedEvent struct {
	Type   string `json:"type"`
	Tool   string `json:"tool"`
	OK     bool   `json:"ok"`
	Output string `json:"output"`
}

type noticeEvent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type errorEvent struct {
	Type  string `json:"type"`
	Error string `json:"error"`
}

type finishedEvent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func (m *textMessenger) emit(event any) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintln(m.out, string(data))
}

func toolCallSummary(call domain.ToolCall) string {
	arguments, err := json.Marshal(call.Arguments)
	if err != nil {
		arguments = []byte("{}")
	}
	return fmt.Sprintf("%s %s", call.Name, string(arguments))
}

func summarize(text string) string {
	text = strings.TrimSpace(text)
	if len(text) > 160 {
		return text[:160] + "..."
	}
	return text
}
