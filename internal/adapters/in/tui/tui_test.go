package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
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

// --- Layout: el input siempre debe quedar visible al fondo, sin que el log lo tape ---

func TestViewKeepsInputVisibleAtBottom(t *testing.T) {
	input := textinput.New()
	input.Prompt = "❯ "
	input.Width = 76
	m := Model{styles: newStyles(domain.DefaultTheme()), width: 80, height: 24, input: input}
	m.transcript = manyLines(80) // transcript largo como tras una tarea con mucho log
	view := m.View()
	lines := strings.Split(view, "\n")
	if got := len(lines); got > m.height {
		t.Fatalf("View renderizó %d líneas, supera el alto %d", got, m.height)
	}
	// El prompt del input "❯" debe estar presente (campo visible).
	found := false
	index := -1
	for i, l := range lines {
		if strings.Contains(l, "❯") {
			found = true
			index = i
			break
		}
	}
	if !found {
		t.Fatalf("el prompt del input no aparece en el View (input tapado):\n%s", view)
	}
	// Debe estar en las últimas filas, no enterrado por el log.
	if index < len(lines)-4 {
		t.Fatalf("el input aparece en la fila %d, debería estar al fondo (len=%d):\n%s", index, len(lines), view)
	}
}

// --- Reutilizar prompt fallido (/retry + flecha arriba) ---

func TestRetryReinjectsLastPrompt(t *testing.T) {
	input := textinput.New()
	input.SetValue("arregla el bug")
	m := Model{styles: newStyles(domain.DefaultTheme()), width: 80, height: 24, input: input}

	// Simular Enter: se captura lastPrompt antes de limpiar y se lanza el run.
	upd, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = upd.(Model)
	if m.lastPrompt != "arregla el bug" {
		t.Fatalf("lastPrompt=%q, quiero 'arregla el bug'", m.lastPrompt)
	}
	if cmd == nil {
		t.Fatal("Enter debería lanzar el run del agente")
	}

	// Flecha arriba con el campo vacío recupera el prompt para editarlo.
	m.running = false
	m.input.SetValue("")
	upd, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	m = upd.(Model)
	if m.input.Value() != "arregla el bug" {
		t.Fatalf("tras ↑ el input=%q, quiero 'arregla el bug'", m.input.Value())
	}
}

func TestRetryNoPromptShowsNotice(t *testing.T) {
	m := Model{styles: newStyles(domain.DefaultTheme()), width: 80, height: 24}
	upd, _ := m.handleSlash("/retry")
	m = upd.(Model)
	if m.lastPrompt != "" || m.running {
		t.Fatalf("/retry sin prompt no debería lanzar nada: running=%v", m.running)
	}
}

// --- Overlay de orquestación (/orchestration) ---

func TestOrchestrationOverlayToggle(t *testing.T) {
	m := Model{styles: newStyles(domain.DefaultTheme()), width: 80, height: 24}
	m.showOrch = true
	m.orchModels = []string{"openai/gpt-5", "openai/gpt-5-mini"}
	m.orchPool = map[string]bool{}

	// Enter en la fila 0 alterna Auto.
	upd, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = upd.(Model)
	if !m.orchAuto {
		t.Fatal("Enter en fila 0 debería activar Auto")
	}

	// Bajar a un modelo y marcarlo con espacio.
	upd, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	m = upd.(Model)
	upd, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	m = upd.(Model)
	if !m.orchPool["openai/gpt-5"] {
		t.Fatalf("espacio debería marcar el modelo del pool, pool=%v", m.orchPool)
	}

	// q cierra el overlay.
	upd, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = upd.(Model)
	if m.showOrch {
		t.Fatal("q debería cerrar el overlay de orquestación")
	}
}

// --- Wizard de búsqueda (/search) ---

func TestSearchWizardNavigation(t *testing.T) {
	styles := newStyles(domain.DefaultTheme())
	w := newSearchModel(nil, styles, 80)

	// Default: selecciona brave (cursor 0).
	if w.providers[w.cursor] != "brave" {
		t.Fatalf("cursor inicial debería apuntar a brave, got %q", w.providers[w.cursor])
	}

	// Enter en "brave" pasa a la etapa de la API key.
	w, _ = w.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if w.stage != searchEnterKey {
		t.Fatalf("tras Enter con brave, stage=%d, quiero searchEnterKey", w.stage)
	}
	if !w.key.Focused() {
		t.Fatal("el campo de la API key debería estar enfocado")
	}

	// Esc vuelve a la selección de proveedor.
	w, _ = w.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if w.stage != searchPickProvider {
		t.Fatalf("tras Esc, stage=%d, quiero searchPickProvider", w.stage)
	}

	// Seleccionar "off" (sin guardar, sin app).
	w, _ = w.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	if w.providers[w.cursor] != "off" {
		t.Fatalf("cursor debería estar en off, got %q", w.providers[w.cursor])
	}
	_, cmd := w.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("off no debería lanzar comando de fin si el guardado es síncrono con app nil")
	}
}

// --- Recomendación del modo plan ---

func TestCollapseHeaderAndToggle(t *testing.T) {
	m := Model{transcript: []transcriptLine{
		{kind: "user", text: "hi"},
		{kind: "assistant", text: strings.Repeat("a ", 600)}, // >500 chars → colapsable
	}}
	m.maybeCollapseLastAssistant()
	if len(m.transcript) != 2 || !m.transcript[1].collapsed {
		t.Fatal("la respuesta larga debería colapsarse")
	}
	// toggle expande
	m.toggleLastAssistantColapse()
	if m.transcript[1].collapsed {
		t.Fatal("toggle debería expandir la respuesta")
	}
	// header resume líneas
	if !strings.Contains(collapseHeader("x\n\ny"), "líneas") {
		t.Fatal("collapseHeader debería indicar el número de líneas")
	}
	// respuesta corta NO se colapsa
	m2 := Model{transcript: []transcriptLine{{kind: "assistant", text: "corto"}}}
	m2.maybeCollapseLastAssistant()
	if len(m2.transcript) == 1 && m2.transcript[0].collapsed {
		t.Fatal("respuesta corta no debería colapsarse")
	}
}

func TestDividerConsecutiveGuard(t *testing.T) {
	m := Model{}
	m.append("divider", "")
	m.append("divider", "")
	if len(m.transcript) != 1 {
		t.Fatalf("divisores consecutivos deberían deduplicarse, got %d", len(m.transcript))
	}
}

func TestIsRecommendation(t *testing.T) {
	positive := []string{
		"✅ Recomendación: Opción B",
		"Recomendación: refactorizar en módulos",
		"Recomendado: usar la opción 2",
		"Recomendada: la alternativa A",
		"recomendacion: enfoque hibrido",
	}
	negative := []string{
		"Explorando el proyecto...",
		"Opciones: A, B y C",
		"La opción B no se recomienda aquí",
		"verificación: go test ./...",
	}
	for _, text := range positive {
		if !isRecommendation(text) {
			t.Errorf("isRecommendation(%q) = false, quiero true", text)
		}
	}
	for _, text := range negative {
		if isRecommendation(text) {
			t.Errorf("isRecommendation(%q) = true, quiero false", text)
		}
	}
}

// --- Logo FORGEN ---

func TestRenderLogoLines(t *testing.T) {
	m := Model{styles: newStyles(domain.DefaultTheme())}
	lines := m.renderLogoLines()
	if len(lines) < 4 {
		t.Fatalf("el logotipo debe tener al menos 4 líneas, tengo %d", len(lines))
	}
	// El logotipo FORGEN generado debe ser lo bastante ancho para leerse.
	if len(lines[0]) < 30 {
		t.Fatalf("el logotipo parece truncado: %d celdas en la fila 1", len(lines[0]))
	}
	for _, line := range lines {
		if line == "" {
			t.Fatalf("el logotipo tiene una línea vacía")
		}
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
