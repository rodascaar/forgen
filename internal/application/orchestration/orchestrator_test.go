package orchestration_test

import (
	"log/slog"
	"testing"

	"github.com/rodascaar/forgen/internal/adapters/out/llm"
	"github.com/rodascaar/forgen/internal/application/orchestration"
	"github.com/rodascaar/forgen/internal/core/domain"
)

func testConfig() domain.AppConfig {
	return domain.AppConfig{
		Providers: []domain.ProviderConfig{
			{Name: "openai", Type: domain.ProviderTypeOpenAICompatible, BaseURL: "https://api.openai.com/v1", Models: []string{"gpt-5", "gpt-5-mini"}},
			{Name: "anthropic", Type: domain.ProviderTypeAnthropic, BaseURL: "https://api.anthropic.com", Models: []string{"claude-sonnet-4-5"}},
		},
		Default: domain.DefaultSelection{Provider: "openai", Model: "gpt-5"},
	}
}

func newOrchestrator(config domain.AppConfig) *orchestration.Orchestrator {
	return orchestration.NewOrchestrator(config, llm.NewFactory(slog.Default()), slog.Default())
}

func TestClassifyPhases(t *testing.T) {
	orchestrator := newOrchestrator(testConfig())

	cases := []struct {
		prompt string
		phase  domain.AgentPhase
	}{
		{"explica cómo funciona este código", domain.PhaseExplore},
		{"diseña el plan y la arquitectura", domain.PhasePlan},
		{"haz review de mi PR y encuentra bugs", domain.PhaseReview},
		{"investiga la documentación de esta API", domain.PhaseResearch},
		{"implementa la función de login", domain.PhaseBuild},
	}
	for _, tc := range cases {
		got := orchestrator.Classify(tc.prompt)
		if got != tc.phase {
			t.Fatalf("Classify(%q) = %q, want %q", tc.prompt, got, tc.phase)
		}
	}
}

func TestSingleModelDefaults(t *testing.T) {
	orchestrator := newOrchestrator(testConfig())

	if orchestrator.IsMultiModel() {
		t.Fatal("sin model_roles configurados no debería ser multi-modelo")
	}
	model := orchestrator.SelectFor(domain.PhaseBuild, "implementa algo")
	if model.Key() != "openai/gpt-5" {
		t.Fatalf("modelo = %q, want openai/gpt-5", model.Key())
	}
}

func TestMultiModelRoleRouting(t *testing.T) {
	config := testConfig()
	config.ModelRoles = map[string][]string{
		"explorer": {"openai/gpt-5-mini"},
		"builder":  {"openai/gpt-5", "anthropic/claude-sonnet-4-5"},
		"reviewer": {"anthropic/claude-sonnet-4-5"},
	}
	config.ModelMetadata = map[string]domain.ModelMetadata{
		"anthropic/claude-sonnet-4-5": {Tier: domain.TierHeavy},
		"openai/gpt-5-mini":           {Tier: domain.TierLight},
	}
	orchestrator := newOrchestrator(config)

	if !orchestrator.IsMultiModel() {
		t.Fatal("debería detectar multi-modelo")
	}

	// Explorar usa el modelo liviano.
	exploreModel := orchestrator.SelectFor(domain.PhaseExplore, "explora el repo")
	if exploreModel.Key() != "openai/gpt-5-mini" {
		t.Fatalf("explore model = %q", exploreModel.Key())
	}

	// Builder simple usa el liviano (gpt-5 default standard vs claude heavy).
	buildModel := orchestrator.SelectFor(domain.PhaseBuild, "añade un campo al struct")
	if buildModel.Key() != "openai/gpt-5" {
		t.Fatalf("build simple model = %q, want openai/gpt-5", buildModel.Key())
	}

	// Builder complejo escala al modelo heavy.
	heavyModel := orchestrator.SelectFor(domain.PhaseBuild, "refactoriza para concurrencia y evita race conditions")
	if heavyModel.Key() != "anthropic/claude-sonnet-4-5" {
		t.Fatalf("build complejo model = %q, want heavy", heavyModel.Key())
	}
}

func TestProviderTags(t *testing.T) {
	config := testConfig()
	orchestrator := newOrchestrator(config)

	model := orchestrator.SelectFor(domain.PhaseBuild, "algo")
	provider, err := orchestrator.Provider(t.Context(), model)
	if err != nil {
		t.Fatalf("Provider: %v", err)
	}
	if provider.Name() != "openai" {
		t.Fatalf("provider = %q", provider.Name())
	}
}
