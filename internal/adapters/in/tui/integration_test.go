package tui

import (
	"errors"
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

// testApp construye una App aislada en directorios temporales.
func testApp(t *testing.T) *apppkg.App {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FORGEN_CONFIG_DIR", filepath.Join(dir, "config"))
	t.Setenv("FORGEN_DATA_DIR", filepath.Join(dir, "data"))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	app, err := apppkg.NewApp(logger)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(app.Close)
	return app
}

// TestWizardDoneClosesSubModel: regresión de la congelación de /init. El
// mensaje que cierra el wizard no debe delegarse al propio wizard (se tragaba).
func TestWizardDoneClosesSubModel(t *testing.T) {
	m := newModel(testApp(t))
	m.wizard = newWizardModel(m.app, m.styles, 80)

	updated, _ := m.Update(wizardDoneMsg{})
	mm, ok := updated.(Model)
	if !ok {
		t.Fatalf("modelo inesperado: %T", updated)
	}
	if mm.wizard != nil {
		t.Fatal("wizardDoneMsg debería cerrar el wizard")
	}
	if !mm.input.Focused() {
		t.Fatal("tras cerrar el wizard, el input debería seguir enfocado")
	}
}

// TestPickerSelectionClosesSubModel: los selectores (/provider, /model,
// /sessions) también se congelaban por el mismo motivo.
func TestPickerSelectionClosesSubModel(t *testing.T) {
	m := newModel(testApp(t))
	m.picker = newPickerModel(pickerProviderKind, "t", []pickerItem{{label: "a", value: "a"}}, m.styles, 80, 24)

	updated, _ := m.Update(pickerCancelledMsg{})
	if mm, ok := updated.(Model); !ok || mm.picker != nil {
		t.Fatal("pickerCancelledMsg debería cerrar el picker")
	}

	m.picker = newPickerModel(pickerModelKind, "t", []pickerItem{{label: "gpt-5", value: "gpt-5"}}, m.styles, 80, 24)
	updated, _ = m.Update(pickerSelectedMsg{kind: pickerModelKind, value: "gpt-5"})
	if mm, ok := updated.(Model); !ok || mm.picker != nil {
		t.Fatal("pickerSelectedMsg debería cerrar el picker")
	}
}

// TestConfirmResetOnRunEnd: si la petición termina/falla con un permiso
// pendiente, el modal Y/N no debe quedar atascado (antes no se podía escribir).
func TestConfirmResetOnRunEnd(t *testing.T) {
	m := newModel(testApp(t))
	m.confirming = true
	m.confirmCh = make(chan bool, 1)
	m.running = true

	updated, _ := m.Update(runDoneMsg{err: errors.New("boom")})
	mm, ok := updated.(Model)
	if !ok {
		t.Fatalf("modelo inesperado: %T", updated)
	}
	if mm.confirming {
		t.Fatal("runDoneMsg con error debería resetear el estado de confirmación")
	}
	if mm.running {
		t.Fatal("runDoneMsg debería marcar running=false")
	}

	// errorMsg también debe resetear el modal.
	m.confirming = true
	updated, _ = m.Update(errorMsg{err: errors.New("boom")})
	if mm, ok := updated.(Model); !ok || mm.confirming {
		t.Fatal("errorMsg debería resetear el estado de confirmación")
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
