package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rodascaar/forgen/internal/adapters/out/llm"
	"github.com/rodascaar/forgen/internal/core/domain"
)

func TestValidateProviderKeyListsModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("ruta inesperada: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]string{
				{"id": "gpt-5"},
				{"id": "gpt-5-mini"},
			},
		})
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(discard{}, nil))
	app, err := NewApp(logger)
	if err != nil {
		t.Fatal(err)
	}
	app.LLMFactory = llm.NewFactory(logger)

	config := domain.ProviderConfig{
		Name:    "fake",
		Type:    domain.ProviderTypeOpenAICompatible,
		BaseURL: server.URL,
	}
	models, err := app.ValidateProviderKey(context.Background(), config, "test-key")
	if err != nil {
		t.Fatalf("ValidateProviderKey: %v", err)
	}
	if len(models) != 2 || models[0] != "gpt-5" {
		t.Fatalf("modelos inesperados: %v", models)
	}
}

func TestValidateProviderKeyFallsBackToConfig(t *testing.T) {
	// Si el endpoint no responde modelos (p.ej. lista vacía), se usa el fallback
	// definido en la config del proveedor.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []any{}})
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(discard{}, nil))
	app, err := NewApp(logger)
	if err != nil {
		t.Fatal(err)
	}
	app.LLMFactory = llm.NewFactory(logger)

	config := domain.ProviderConfig{
		Name:    "fake",
		Type:    domain.ProviderTypeOpenAICompatible,
		BaseURL: server.URL,
		Models:  []string{"llama3"},
	}
	models, err := app.ValidateProviderKey(context.Background(), config, "test-key")
	if err != nil {
		t.Fatalf("ValidateProviderKey: %v", err)
	}
	if len(models) != 1 || models[0] != "llama3" {
		t.Fatalf("debería usar el fallback de config, got %v", models)
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func TestProviderUsable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FORGEN_CONFIG_DIR", filepath.Join(dir, "config"))
	t.Setenv("FORGEN_DATA_DIR", filepath.Join(dir, "data"))
	logger := slog.New(slog.NewTextHandler(discard{}, nil))
	app, err := NewApp(logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)

	// Ollama local sin key debe ser usable (no requiere API key).
	ollama := domain.ProviderConfig{
		Name:      "ollama",
		Type:      domain.ProviderTypeOpenAICompatible,
		BaseURL:   "http://localhost:11434/v1",
		APIKeyEnv: "",
	}
	if !app.ProviderUsable(ollama) {
		t.Fatal("ollama local sin key debería ser usable")
	}

	// Proveedor remoto sin key no debe ser usable.
	remote := domain.ProviderConfig{
		Name:      "openai",
		Type:      domain.ProviderTypeOpenAICompatible,
		BaseURL:   "https://api.openai.com/v1",
		APIKeyEnv: "FORGEN_TEST_NO_SUCH_KEY_XYZ",
	}
	if app.ProviderUsable(remote) {
		t.Fatal("proveedor remoto sin key no debería ser usable")
	}
}
