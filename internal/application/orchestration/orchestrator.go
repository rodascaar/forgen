// Package orchestration implementa el routing multi-modelo por roles y fases.
package orchestration

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/rodascaar/forgen/internal/adapters/out/llm"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// phaseTagHeader y modelTagHeader se envían a los gateways para atribución.
const (
	phaseTagHeader = "x-forgen-phase"
	modelTagHeader = "x-forgen-model"
)

// complexityKeywords sugieren un modelo heavy para tareas complejas (bilíngüe).
var complexityKeywords = []string{
	"concurren", "algoritmo", "race", "deadlock", "refactor", "arquitect", "architecture",
	"escalab", "optimiz", "perf", "debug", "seguridad", "security", "migra", "migration",
	"concurrency", "parallel", "async", "database", "transaction", "auth", "jwt",
	"test coverage", "benchmark", "scale", "distributed", "microservice",
}

// Orchestrator clasifica tareas por fase y elige el modelo por rol.
type Orchestrator struct {
	config     domain.AppConfig
	factory    *llm.Factory
	resolveKey func(domain.ProviderConfig) string
	logger     *slog.Logger
	phase      domain.AgentPhase
	model      domain.Model
}

// NewOrchestrator construye el orquestador con la configuración efectiva.
// resolveKey resuelve la API key del proveedor (CredentialStore + env fallback).
func NewOrchestrator(config domain.AppConfig, factory *llm.Factory, resolveKey func(domain.ProviderConfig) string, logger *slog.Logger) *Orchestrator {
	if resolveKey == nil {
		resolveKey = func(pc domain.ProviderConfig) string { return pc.ResolveAPIKey(os.Getenv) }
	}
	return &Orchestrator{
		config:     config,
		factory:    factory,
		resolveKey: resolveKey,
		logger:     logger,
		phase:      domain.PhaseBuild,
		model:      domain.Model{Provider: config.Default.Provider, ID: config.Default.Model},
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
	provider, err := o.factory.CreateWithKeyResolver(providerConfig, o.resolveKey, tags)
	if err != nil {
		return nil, err
	}
	o.logger.Info("orchestration.select", "phase", o.phase, "model", model.Key(), "provider", provider.Name())
	return provider, nil
}

// roleModels devuelve los modelos configurados para un rol. Si el rol no tiene
// modelos explícitos (model_roles) y el routing automático está activo, usa el
// pool del proveedor por defecto. En último caso, el modelo único por defecto.
func (o *Orchestrator) roleModels(role domain.ModelRole) []domain.Model {
	keys, ok := o.config.ModelRoles[string(role)]
	if ok && len(keys) > 0 {
		models := make([]domain.Model, 0, len(keys))
		for _, key := range keys {
			if model, ok := parseModelKey(key, o.config); ok {
				models = append(models, model)
			}
		}
		if len(models) > 0 {
			return models
		}
	}
	// Routing automático: pool del proveedor por defecto (una sola API key).
	if pool := o.autoPool(); len(pool) > 0 {
		return pool
	}
	return []domain.Model{o.defaultModel()}
}

func (o *Orchestrator) defaultModel() domain.Model {
	return domain.Model{Provider: o.config.Default.Provider, ID: o.config.Default.Model}
}

// autoPool construye el pool de modelos del proveedor por defecto cuando el
// routing automático está activo. Usa los modelos elegidos en
// orchestration.pool o, si está vacío, todos los disponibles del proveedor.
func (o *Orchestrator) autoPool() []domain.Model {
	if !o.config.Orchestration.Auto {
		return nil
	}
	provider := o.config.Default.Provider
	var ids []string
	if len(o.config.Orchestration.Pool) > 0 {
		ids = o.config.Orchestration.Pool
	} else if pc, ok := o.config.FindProvider(provider); ok {
		ids = pc.Models
	}
	if len(ids) == 0 {
		return nil
	}
	pool := make([]domain.Model, 0, len(ids))
	for _, id := range ids {
		if model, ok := o.parsePoolModel(id); ok {
			pool = append(pool, model)
		}
	}
	return pool
}

// parsePoolModel acepta una entrada del pool: "provider/model" o solo el ID
// del modelo (se asume el proveedor por defecto).
func (o *Orchestrator) parsePoolModel(id string) (domain.Model, bool) {
	if strings.Contains(id, "/") {
		return parseModelKey(id, o.config)
	}
	return domain.Model{Provider: o.config.Default.Provider, ID: id}, true
}

// pickFromPool elige por scoring agnóstico: 0→light, 1→standard, 2+→heavy.
func (o *Orchestrator) pickFromPool(pool []domain.Model, prompt string) domain.Model {
	if len(pool) == 1 {
		return pool[0]
	}
	score := complexityScore(prompt)
	sorted := make([]domain.Model, len(pool))
	copy(sorted, pool)
	sort.SliceStable(sorted, func(i, j int) bool {
		return tierWeight(o.tierOf(sorted[i])) < tierWeight(o.tierOf(sorted[j]))
	})
	if score >= 2 {
		return sorted[len(sorted)-1]
	}
	if score == 1 && len(sorted) >= 2 {
		// middle tier si existe
		return sorted[len(sorted)/2]
	}
	return sorted[0]
}

func (o *Orchestrator) tierOf(model domain.Model) domain.Tier {
	if metadata, ok := o.config.ModelMetadata[model.Key()]; ok && metadata.Tier != "" {
		return metadata.Tier
	}
	return inferTier(model.ID)
}

// inferTier deduce el nivel de un modelo por heurística de nombre. Se usa como
// fallback cuando no hay model_metadata explícita, para que el routing por
// complejidad funcione sin configurar tiers a mano. Override-able con
// model_metadata.
func inferTier(id string) domain.Tier {
	id = strings.ToLower(id)
	heavy := []string{"pro", "opus", "ultra", "max", "large", "405b", "253b", "120b", "70b", "qwen-max", "deepseek-r1", "nemotron-ultra", "gigant"}
	for _, kw := range heavy {
		if strings.Contains(id, kw) {
			return domain.TierHeavy
		}
	}
	light := []string{"mini", "nano", "flash", "haiku", "small", "lite", "light", "1b", "3b", "7b", "8b", "12b", "fast"}
	for _, kw := range light {
		if strings.Contains(id, kw) {
			return domain.TierLight
		}
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

//nolint:unused // kept for external callers, internal uses complexityScore
func isComplex(prompt string) bool { return complexityScore(prompt) >= 2 }

func complexityScore(prompt string) int {
	lower := strings.ToLower(prompt)
	score := 0
	for _, kw := range complexityKeywords {
		if strings.Contains(lower, kw) {
			score++
		}
	}
	// Heurísticas adicionales agnósticas
	if len(prompt) > 300 {
		score++
	}
	if strings.Contains(lower, "3+") || strings.Contains(lower, "varios archivos") || strings.Contains(lower, "multiple files") {
		score++
	}
	// Mención de paths múltiples
	if strings.Count(lower, "/") >= 3 {
		score++
	}
	return score
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
