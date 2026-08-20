// Package orchestration implementa el routing multi-modelo por roles y fases.
package orchestration

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/forgen/forgen/internal/adapters/out/llm"
	"github.com/forgen/forgen/internal/core/domain"
	"github.com/forgen/forgen/internal/core/ports"
)

// phaseTagHeader y modelTagHeader se envían a los gateways para atribución.
const (
	phaseTagHeader = "x-forgen-phase"
	modelTagHeader = "x-forgen-model"
)

// complexityKeywords sugieren un modelo heavy para tareas complejas.
var complexityKeywords = []string{
	"concurren", "algoritmo", "race", "deadlock", "refactor", "arquitect",
	"escalab", "optimiz", "perf", "debug", "seguridad", "migra",
}

// Orchestrator clasifica tareas por fase y elige el modelo por rol.
type Orchestrator struct {
	config  domain.AppConfig
	factory *llm.Factory
	getenv  func(string) string
	logger  *slog.Logger
	phase   domain.AgentPhase
	model   domain.Model
}

// NewOrchestrator construye el orquestador con la configuración efectiva.
func NewOrchestrator(config domain.AppConfig, factory *llm.Factory, logger *slog.Logger) *Orchestrator {
	return &Orchestrator{
		config:  config,
		factory: factory,
		getenv:  os.Getenv,
		logger:  logger,
		phase:   domain.PhaseBuild,
		model:   domain.Model{Provider: config.Default.Provider, ID: config.Default.Model},
	}
}

// Phase devuelve la fase actual.
func (o *Orchestrator) Phase() domain.AgentPhase { return o.phase }

// Model devuelve el modelo seleccionado actual.
func (o *Orchestrator) Model() domain.Model { return o.model }

// SetPhase fija la fase actual (tracking).
func (o *Orchestrator) SetPhase(phase domain.AgentPhase) { o.phase = phase }

// IsMultiModel indica si hay más de un modelo configurado entre los roles.
func (o *Orchestrator) IsMultiModel() bool {
	seen := map[string]bool{}
	for _, role := range domain.DefaultRoles() {
		for _, model := range o.roleModels(role) {
			seen[model.Key()] = true
		}
	}
	return len(seen) > 1
}

// Classify clasifica un prompt en una fase por heurística determinista.
func (o *Orchestrator) Classify(prompt string) domain.AgentPhase {
	lower := strings.ToLower(prompt)

	if containsAny(lower, "explica", "cómo funciona", "entender", "revisa el código", "analiza", "explora", "qué hace") {
		return domain.PhaseExplore
	}
	if containsAny(lower, "plan", "diseñ", "arquitectura", "especific", "pasos", "estrategia", "spec") {
		return domain.PhasePlan
	}
	if containsAny(lower, "review", "revisa mi", "audita", "encuentra bugs", "pr", "code review") {
		return domain.PhaseReview
	}
	if containsAny(lower, "investiga", "busca", "documentación", "research", "última versión", "cómo se usa") {
		return domain.PhaseResearch
	}
	return domain.PhaseBuild
}

// SelectFor elige el modelo para una fase, aplicando tier routing.
func (o *Orchestrator) SelectFor(phase domain.AgentPhase, prompt string) domain.Model {
	role := roleForPhase(phase)
	pool := o.roleModels(role)
	if len(pool) == 0 {
		return o.defaultModel()
	}
	o.phase = phase
	o.model = o.pickFromPool(pool, prompt)
	return o.model
}

// Provider crea el provider para el modelo con tags de fase/modelo.
func (o *Orchestrator) Provider(ctx context.Context, model domain.Model) (ports.LLMProvider, error) {
	providerConfig, ok := o.config.FindProvider(model.Provider)
	if !ok {
		return nil, fmt.Errorf("proveedor %q no configurado", model.Provider)
	}
	tags := map[string]string{
		phaseTagHeader: string(o.phase),
		modelTagHeader: model.Key(),
	}
	provider, err := o.factory.CreateWithTags(providerConfig, o.getenv, tags)
	if err != nil {
		return nil, err
	}
	o.logger.Info("orchestration.select", "phase", o.phase, "model", model.Key(), "provider", provider.Name())
	return provider, nil
}

// roleModels devuelve los modelos configurados para un rol (default = modelo único).
func (o *Orchestrator) roleModels(role domain.ModelRole) []domain.Model {
	keys, ok := o.config.ModelRoles[string(role)]
	if !ok || len(keys) == 0 {
		return []domain.Model{o.defaultModel()}
	}
	models := make([]domain.Model, 0, len(keys))
	for _, key := range keys {
		if model, ok := parseModelKey(key, o.config); ok {
			models = append(models, model)
		}
	}
	if len(models) == 0 {
		return []domain.Model{o.defaultModel()}
	}
	return models
}

func (o *Orchestrator) defaultModel() domain.Model {
	return domain.Model{Provider: o.config.Default.Provider, ID: o.config.Default.Model}
}

// pickFromPool elige el modelo más liviano para tareas simples y escala a
// heavy para tareas complejas o como retry.
func (o *Orchestrator) pickFromPool(pool []domain.Model, prompt string) domain.Model {
	if len(pool) == 1 {
		return pool[0]
	}
	complex := isComplex(prompt)

	// Ordenar por tier (light < standard < heavy).
	sorted := make([]domain.Model, len(pool))
	copy(sorted, pool)
	sort.SliceStable(sorted, func(i, j int) bool {
		return tierWeight(o.tierOf(sorted[i])) < tierWeight(o.tierOf(sorted[j]))
	})

	if complex {
		return sorted[len(sorted)-1] // el más pesado
	}
	return sorted[0] // el más liviano
}

func (o *Orchestrator) tierOf(model domain.Model) domain.Tier {
	if metadata, ok := o.config.ModelMetadata[model.Key()]; ok && metadata.Tier != "" {
		return metadata.Tier
	}
	return domain.TierStandard
}

func tierWeight(tier domain.Tier) int {
	switch tier {
	case domain.TierLight:
		return 0
	case domain.TierStandard:
		return 1
	case domain.TierHeavy:
		return 2
	default:
		return 1
	}
}

func isComplex(prompt string) bool {
	lower := strings.ToLower(prompt)
	for _, keyword := range complexityKeywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

// roleForPhase mapea una fase a su rol dominante.
func roleForPhase(phase domain.AgentPhase) domain.ModelRole {
	switch phase {
	case domain.PhaseExplore:
		return domain.RoleExplorer
	case domain.PhasePlan:
		return domain.RolePlanner
	case domain.PhaseReview:
		return domain.RoleReviewer
	case domain.PhaseResearch:
		return domain.RoleResearcher
	default:
		return domain.RoleBuilder
	}
}

// parseModelKey parsea "provider/model" contra la configuración.
func parseModelKey(key string, config domain.AppConfig) (domain.Model, bool) {
	provider, model, found := strings.Cut(key, "/")
	if !found || provider == "" || model == "" {
		return domain.Model{}, false
	}
	if _, ok := config.FindProvider(provider); !ok {
		return domain.Model{}, false
	}
	return domain.Model{Provider: provider, ID: model}, true
}

func containsAny(text string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}
