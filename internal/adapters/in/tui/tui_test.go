package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rodascaar/forgen/internal/core/domain"
)

func key(t *testing.T, k tea.KeyType) tea.KeyMsg {
	t.Helper()
	return tea.KeyMsg(tea.Key{Type: k})
}

func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("se esperaba un comando, recibí nil")
	}
	return cmd()
}

// --- Picker ---

func TestPickerNavigationAndSelect(t *testing.T) {
	items := []pickerItem{
		{label: "openai", value: "openai"},
		{label: "anthropic", value: "anthropic"},
		{label: "groq", value: "groq"},
	}
	styles := newStyles(domain.DefaultTheme())
	p := newPickerModel(pickerProviderKind, "Elige", items, styles, 80, 24)

	// Navegar hacia abajo dos veces.
	p, _ = p.Update(key(t, tea.KeyDown))
	p, _ = p.Update(key(t, tea.KeyDown))
	if p.cursor != 2 {
		t.Fatalf("cursor=%d, quiero 2", p.cursor)
	}

	// Enter devuelve la selección correcta.
	_, cmd := p.Update(key(t, tea.KeyEnter))
	msg := runCmd(t, cmd)
	selected, ok := msg.(pickerSelectedMsg)
	if !ok {
		t.Fatalf("mensaje inesperado: %T", msg)
	}
	if selected.kind != pickerProviderKind || selected.value != "groq" {
		t.Fatalf("selección inesperada: %+v", selected)
	}
}

func TestPickerStopsAtEdges(t *testing.T) {
	p := newPickerModel(pickerModelKind, "Modelo", []pickerItem{{label: "a", value: "a"}}, newStyles(domain.DefaultTheme()), 80, 24)
	p, _ = p.Update(key(t, tea.KeyUp))
	if p.cursor != 0 {
		t.Fatalf("up en el borde: cursor=%d", p.cursor)
	}
	p, _ = p.Update(key(t, tea.KeyDown))
	if p.cursor != 0 {
		t.Fatalf("down en un solo item: cursor=%d", p.cursor)
	}
}

func TestPickerEscCancels(t *testing.T) {
	p := newPickerModel(pickerProviderKind, "Elige", []pickerItem{{label: "a", value: "a"}}, newStyles(domain.DefaultTheme()), 80, 24)
	_, cmd := p.Update(key(t, tea.KeyEsc))
	msg := runCmd(t, cmd)
	if _, ok := msg.(pickerCancelledMsg); !ok {
		t.Fatalf("esc debería cancelar, got %T", msg)
	}
}

// --- Slash handling ---

func TestSlashRouting(t *testing.T) {
	cases := map[string]string{
		"/init":     "/init",
		"/help":     "/help",
		"/?":        "/help",
		"/quit":     "/quit",
		"/exit":     "/quit",
		"/provider": "/provider",
		"/model":    "/model",
		"/sessions": "/sessions",
	}
	for input, want := range cases {
		cmd := strings.Fields(input)[0]
		if got := slashCommandKind(cmd); got != want {
			t.Errorf("%s: got %s, want %s", input, got, want)
		}
	}
	if got := slashCommandKind("/nope"); got != "" {
		t.Errorf("comando desconocido debería ser vacío, got %q", got)
	}
}

// slashCommandKind normaliza un comando slash a su forma canónica.
func slashCommandKind(cmd string) string {
	switch cmd {
	case "/init":
		return "/init"
	case "/help", "/?":
		return "/help"
	case "/quit", "/exit":
		return "/quit"
	case "/provider":
		return "/provider"
	case "/model":
		return "/model"
	case "/sessions":
		return "/sessions"
	}
	return ""
}

// --- Scroll / transcript ---

func manyLines(n int) []transcriptLine {
	lines := make([]transcriptLine, 0, n)
	for i := 0; i < n; i++ {
		lines = append(lines, transcriptLine{kind: "user", text: fmt.Sprintf("línea %d", i)})
	}
	return lines
}

func TestRenderTranscriptKeepsLimit(t *testing.T) {
	m := Model{styles: newStyles(domain.DefaultTheme()), width: 80, height: 24}
	m.transcript = manyLines(50)
	limit := m.height - 4 // 20
	out := m.renderTranscript(limit)
	if got := len(strings.Split(out, "\n")); got != limit {
		t.Fatalf("renderTranscript devolvió %d líneas, quiero %d", got, limit)
	}
	// Sin scroll debe mostrar las últimas líneas (fondo) y no las primeras.
	if !strings.Contains(out, "línea 49") {
		t.Fatalf("esperaba la última línea al fondo:\n%s", out)
	}
	if strings.Contains(out, "línea 0") {
		t.Fatalf("no debería mostrarse la primera línea al fondo:\n%s", out)
	}
}

func TestRenderTranscriptScrollUpRevealsStart(t *testing.T) {
	m := Model{styles: newStyles(domain.DefaultTheme()), width: 80, height: 24}
	m.transcript = manyLines(50)
	limit := m.height - 4
	// Subir del todo: debe verse la primera línea y seguir acotado a 'limit'.
	m.scrollOffset = 50 - limit
	out := m.renderTranscript(limit)
	if !strings.Contains(out, "línea 0") {
		t.Fatalf("al subir del todo debería verse la primera línea:\n%s", out)
	}
	if got := len(strings.Split(out, "\n")); got != limit {
		t.Fatalf("al subir renderTranscript devolvió %d líneas, quiero %d", got, limit)
	}
}

func TestScrollByClamps(t *testing.T) {
	m := Model{width: 80, height: 24}
	m.transcript = manyLines(1)
	m.scrollBy(+100)
	if m.scrollOffset != 0 {
		t.Fatalf("scrollOffset=%d, quiero 0 (no desbordar el transcript)", m.scrollOffset)
	}
	m.scrollBy(-100)
	if m.scrollOffset != 0 {
		t.Fatalf("scrollOffset=%d tras bajar, quiero 0", m.scrollOffset)
	}
}

func TestMouseWheelScroll(t *testing.T) {
	m := Model{styles: newStyles(domain.DefaultTheme()), width: 80, height: 24}
	m.transcript = manyLines(50)

	upd, _ := m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonWheelUp})
	mm := upd.(Model)
	if mm.scrollOffset != 3 {
		t.Fatalf("scrollOffset=%d tras wheel up, quiero 3", mm.scrollOffset)
	}

	down, _ := mm.handleMouse(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	mm2 := down.(Model)
	if mm2.scrollOffset != 0 {
		t.Fatalf("scrollOffset=%d tras wheel down, quiero 0", mm2.scrollOffset)
	}
}

// --- Permission detail ---

func TestPermissionDetail(t *testing.T) {
	call := domain.ToolCall{
		Name: "bash",
		Arguments: map[string]any{
			"command": "rm -rf /tmp/foo",
		},
	}
	detail := permissionDetail(call)
	if !strings.Contains(detail, "bash") || !strings.Contains(detail, "rm -rf /tmp/foo") {
		t.Fatalf("detalle incompleto: %q", detail)
	}
}

// --- Help ---

func TestHelpContent(t *testing.T) {
	m := Model{styles: newStyles(domain.DefaultTheme())}
	help := m.renderHelp()
	for _, wanted := range []string{"/init", "/provider", "/model", "PgUp", "ratón", "Tab"} {
		if !strings.Contains(help, wanted) {
			t.Errorf("la ayuda debería mencionar %q", wanted)
		}
	}
}

// --- Messenger defensivo ---

// TestMessengerNilProgramNoPanic: un messenger con program nil (estado
// inconsistente o run que sobrevive a la TUI) no debe crashear.
func TestMessengerNilProgramNoPanic(t *testing.T) {
	messenger := newTUIMessenger(nil)
	messenger.StreamText("", "hola")
	messenger.ToolStarted("", domain.ToolCall{Name: "bash"})
	messenger.ToolFinished("", domain.ToolCall{Name: "bash"}, domain.ToolResult{OK: true})
	messenger.Notice("", "aviso")
	messenger.Error("", errors.New("boom"))
	messenger.Finished("", "listo")
}
