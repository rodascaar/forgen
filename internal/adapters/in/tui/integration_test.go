package tui

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	apppkg "github.com/rodascaar/forgen/internal/app"
)

// toJSON serializa v para las líneas SSE del proveedor fake.
func toJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

// TestTUIStreamingNoPanic: test de humo para verificar que el fix del
// puntero (tea.NewProgram(&model, ...)) elimina el crash nil-pointer
// al hacer streaming. Se ejecuta con un proveedor SSE fake que responde
// inmediatamente, y se verifica que el programa no crashea.
// No se verifica el contenido completo del transcript (eso requeriría
// una infraestructura más pesada); el test crítico es que no crashee.
func TestTUIStreamingNoPanic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := map[string]any{
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{"content": "respuesta"}, "finish_reason": nil}},
		}
		done := map[string]any{
			"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 5, "completion_tokens": 1},
		}
		_, _ = fmt.Fprintf(w, "data: %s\ndata: %s\ndata: [DONE]\n", toJSON(chunk), toJSON(done))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "config")
	_ = os.MkdirAll(cfgDir, 0o755)
	t.Setenv("FORGEN_CONFIG_DIR", cfgDir)
	t.Setenv("FORGEN_DATA_DIR", filepath.Join(dir, "data"))
	config := fmt.Sprintf("providers:\n  - name: openai\n    type: openai_compatible\n    base_url: %s\n    api_key_env: FORGEN_TEST_KEY\n    models: [gpt-5]\ndefault:\n  provider: openai\n  model: gpt-5\n", server.URL)
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FORGEN_TEST_KEY", "test-key")

	app, err := apppkg.NewApp(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	model := newModel(app)
	program := tea.NewProgram(&model,
		tea.WithoutRenderer(),
		tea.WithInput(strings.NewReader("hola\n")),
		tea.WithOutput(io.Discard),
	)
	model.program = program

	done := make(chan error, 1)
	go func() {
		_, err := program.Run()
		done <- err
	}()

	// Dar tiempo al run (clasificación + stream + done). 3s es amplio para fake local.
	time.Sleep(2500 * time.Millisecond)
	program.Quit()

	select {
	case runErr := <-done:
		if runErr != nil {
			t.Fatalf("el programa crasheó: %v", runErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout esperando al programa")
	}
	// Si llegamos aquí sin crash, el fix funciona (no nil deref en StreamText).
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
