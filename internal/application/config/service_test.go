package config_test

import (
	"context"
	"testing"

	"github.com/forgen/forgen/internal/application/config"
	"github.com/forgen/forgen/internal/core/domain"
)

// fakeConfigStore devuelve una configuración fija.
type fakeConfigStore struct {
	config domain.AppConfig
}

func (f *fakeConfigStore) Load(context.Context) (domain.AppConfig, error) { return f.config, nil }
func (f *fakeConfigStore) Save(context.Context, domain.AppConfig) error   { return nil }
func (f *fakeConfigStore) Path() string                                   { return "/fake" }

func TestLoadDefaultsWhenNoFile(t *testing.T) {
	service := config.NewService(&fakeConfigStore{}, func(string) string { return "" }, config.Overrides{})
	loaded, err := service.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Agent != "build" {
		t.Fatalf("Agent por defecto = %q, want build", loaded.Agent)
	}
	if len(loaded.Providers) == 0 {
		t.Fatal("esperaba proveedores por defecto")
	}
}

func TestFileOverridesDefaults(t *testing.T) {
	store := &fakeConfigStore{config: domain.AppConfig{
		Providers: []domain.ProviderConfig{{Name: "local", Type: domain.ProviderTypeOpenAICompatible, BaseURL: "http://localhost", Models: []string{"llama"}}},
		Default:   domain.DefaultSelection{Provider: "local", Model: "llama"},
		Agent:     "plan",
	}}
	service := config.NewService(store, func(string) string { return "" }, config.Overrides{})
	loaded, err := service.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Agent != "plan" {
		t.Fatalf("Agent = %q, want plan", loaded.Agent)
	}
	if loaded.Default.Provider != "local" {
		t.Fatalf("Provider = %q, want local", loaded.Default.Provider)
	}
}

func TestEnvAndOverridesWin(t *testing.T) {
	store := &fakeConfigStore{}
	env := func(key string) string {
		if key == "FORGEN_PROVIDER" {
			return "anthropic"
		}
		return ""
	}
	// Sin flags: gana el env.
	envService := config.NewService(store, env, config.Overrides{})
	loaded, err := envService.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Default.Provider != "anthropic" {
		t.Fatalf("Provider = %q, want anthropic (desde env)", loaded.Default.Provider)
	}

	// Con flags: el flag gana sobre el env.
	flagService := config.NewService(store, env, config.Overrides{Provider: "openai"})
	loaded, err = flagService.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Default.Provider != "openai" {
		t.Fatalf("Provider = %q, want openai (desde flag)", loaded.Default.Provider)
	}
}

func TestResolveModelValidation(t *testing.T) {
	appConfig := domain.AppConfig{
		Providers: []domain.ProviderConfig{
			{Name: "openai", Type: domain.ProviderTypeOpenAICompatible, Models: []string{"gpt-5"}},
		},
		Default: domain.DefaultSelection{Provider: "openai", Model: "gpt-5"},
	}
	if _, err := config.ResolveModel(appConfig, "openai", "gpt-5"); err != nil {
		t.Fatalf("modelo válido rechazado: %v", err)
	}
	if _, err := config.ResolveModel(appConfig, "openai", "gpt-4"); err == nil {
		t.Fatal("esperaba error para modelo no listado")
	}
	if _, err := config.ResolveModel(appConfig, "missing", ""); err == nil {
		t.Fatal("esperaba error para proveedor inexistente")
	}
}
