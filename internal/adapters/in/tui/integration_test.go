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
