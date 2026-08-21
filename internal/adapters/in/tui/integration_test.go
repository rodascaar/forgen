package tui

import (
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	apppkg "github.com/rodascaar/forgen/internal/app"
)

// TestTypingAppendsToFocusedInput prueba que escribir produce texto en el campo
// de entrada: el input queda enfocado en newModel y handleKey lo alimenta.
// Regresión del bug "no logro escribir".
func TestTypingAppendsToFocusedInput(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FORGEN_CONFIG_DIR", filepath.Join(dir, "config"))
	t.Setenv("FORGEN_DATA_DIR", filepath.Join(dir, "data"))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := apppkg.NewApp(logger)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	m := newModel(app)
	if !m.input.Focused() {
		t.Fatal("el input debería quedar enfocado")
	}

	// Simular tecleado de "hola".
	typed := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hola")}
	updated, _ := m.Update(typed)
	mm, ok := updated.(Model)
	if !ok {
		t.Fatalf("modelo inesperado: %T", updated)
	}
	if got := mm.input.Value(); got != "hola" {
		t.Fatalf("al escribir, input=%q, quiero %q", got, "hola")
	}
}


// TestNewUserOnboardingPersistsAndInitOpensWizard valida la métrica de éxito:
// un usuario nuevo, sin config, ve la guía de /init (el fix de Init que antes
// se perdía) y /init abre el asistente de configuración.
func TestNewUserOnboardingPersistsAndInitOpensWizard(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FORGEN_CONFIG_DIR", filepath.Join(dir, "config"))
	t.Setenv("FORGEN_DATA_DIR", filepath.Join(dir, "data"))

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := apppkg.NewApp(logger)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	defer app.Close()

	m := newModel(app)

	// 0. El input debe quedar enfocado (bug de "no poder escribir": Init() con
	//    receiver por valor descartaba el Focus).
	if !m.input.Focused() {
		t.Fatal("el input debería quedar enfocado tras newModel")
	}

	// 1. El banner de onboarding debe persistir en el transcript (bug arreglado).
	var text strings.Builder
	for _, line := range m.transcript {
		text.WriteString(line.text)
		text.WriteString("\n")
	}
	if !strings.Contains(text.String(), "/init") {
		t.Fatalf("el mensaje de onboarding no persistió:\n%s", text.String())
	}
	if !m.noConfig {
		t.Fatal("con config vacía, noConfig debería ser true")
	}

	// 2. Escribir /init y enviarlo abre el asistente.
	m.input.SetValue("/init")
	updated, _ := m.Update(key(t, tea.KeyEnter))
	mm, ok := updated.(Model)
	if !ok {
		t.Fatalf("modelo inesperado: %T", updated)
	}
	if mm.wizard == nil {
		t.Fatal("se esperaba que /init abriera el wizard")
	}

	// 3. El selector de sesiones sin sesiones muestra una nota, no un error.
	m2 := newModel(app)
	m2.input.SetValue("/sessions")
	updated2, _ := m2.Update(key(t, tea.KeyEnter))
	mm2, ok := updated2.(Model)
	if !ok {
		t.Fatalf("modelo inesperado: %T", updated2)
	}
	if mm2.picker != nil {
		t.Fatal("sin sesiones no debería abrirse el picker")
	}
}
