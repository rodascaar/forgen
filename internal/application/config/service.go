// Package config contiene el caso de uso de configuración en capas.
package config

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// Service aplica las capas de precedencia: defaults < archivo < env < overrides.
type Service struct {
	store     ports.ConfigStore
	getenv    func(string) string
	overrides Overrides
}

// Overrides agrupa valores de mayor precedencia (flags de CLI).
type Overrides struct {
	Provider string
	Model    string
	Agent    string
}

// NewService construye el servicio con su store y resolución de env.
func NewService(store ports.ConfigStore, getenv func(string) string, overrides Overrides) *Service {
	if getenv == nil {
		getenv = os.Getenv
	}
	return &Service{store: store, getenv: getenv, overrides: overrides}
}

// Load devuelve la configuración efectiva tras aplicar todas las capas.
func (s *Service) Load(ctx context.Context) (domain.AppConfig, error) {
	config := domain.DefaultAppConfig()

	fileConfig, err := s.store.Load(ctx)
	if err != nil {
		return domain.AppConfig{}, fmt.Errorf("leer configuración: %w", err)
	}
	config = s.mergeFile(config, fileConfig)

	config = s.applyEnv(config)
	config = s.applyOverrides(config)

	if err := config.Validate(); err != nil {
		return domain.AppConfig{}, fmt.Errorf("configuración inválida: %w", err)
	}
	return config, nil
}

// Save persiste la configuración en el store.
func (s *Service) Save(ctx context.Context, config domain.AppConfig) error {
	return s.store.Save(ctx, config)
}

// Path devuelve la ruta del archivo de configuración.
func (s *Service) Path() string { return s.store.Path() }

func (s *Service) mergeFile(base, file domain.AppConfig) domain.AppConfig {
	if len(file.Providers) > 0 {
		base.Providers = file.Providers
	}
	if file.Default.Provider != "" {
		base.Default.Provider = file.Default.Provider
	}
	if file.Default.Model != "" {
		base.Default.Model = file.Default.Model
	}
	if file.Permissions.Mode != "" {
		base.Permissions.Mode = file.Permissions.Mode
	}
	if len(file.Permissions.Rules) > 0 {
		base.Permissions.Rules = file.Permissions.Rules
	}
	if file.Agent != "" {
		base.Agent = file.Agent
	}
	if file.MaxIterations > 0 {
		base.MaxIterations = file.MaxIterations
	}
	if file.MaxOutputChars > 0 {
		base.MaxOutputChars = file.MaxOutputChars
	}
	if len(file.ModelRoles) > 0 {
		base.ModelRoles = file.ModelRoles
	}
	if len(file.ModelMetadata) > 0 {
		base.ModelMetadata = file.ModelMetadata
	}
	if len(file.MCPServers) > 0 {
		base.MCPServers = file.MCPServers
	}
	if file.Search.Provider != "" {
		base.Search = file.Search
	}
	if file.Theme != (domain.Theme{}) {
		base.Theme = file.Theme
	}
	if file.Execution != (domain.ExecutionConfig{}) {
		base.Execution = file.Execution
	}
	if file.Language != "" {
		base.Language = file.Language
	}
	if file.ReasoningEffort != "" {
		base.ReasoningEffort = file.ReasoningEffort
	}
	if file.Orchestration.Auto || len(file.Orchestration.Pool) > 0 {
		base.Orchestration = file.Orchestration
	}
	if file.Compaction.Threshold != 0 || file.Compaction.Disabled {
		base.Compaction = file.Compaction
	}
	return base
}

func (s *Service) applyEnv(config domain.AppConfig) domain.AppConfig {
	if value := s.getenv("FORGEN_PROVIDER"); value != "" {
		config.Default.Provider = value
	}
	if value := s.getenv("FORGEN_MODEL"); value != "" {
		config.Default.Model = value
	}
	if value := s.getenv("FORGEN_AGENT"); value != "" {
		config.Agent = value
	}
	if value := s.getenv("FORGEN_PERMISSION_MODE"); value != "" {
		config.Permissions.Mode = value
	}
	return config
}

func (s *Service) applyOverrides(config domain.AppConfig) domain.AppConfig {
	if s.overrides.Provider != "" {
		config.Default.Provider = s.overrides.Provider
	}
	if s.overrides.Model != "" {
		config.Default.Model = s.overrides.Model
	}
	if s.overrides.Agent != "" {
		config.Agent = s.overrides.Agent
	}
	return config
}

// ProviderNames devuelve los nombres de proveedores configurados ordenados.
func ProviderNames(config domain.AppConfig) []string {
	names := make([]string, 0, len(config.Providers))
	for _, provider := range config.Providers {
		names = append(names, provider.Name)
	}
	sort.Strings(names)
	return names
}

// ResolveModel combina proveedor y modelo con validación.
func ResolveModel(config domain.AppConfig, providerName, modelID string) (domain.Model, error) {
	if providerName == "" {
		providerName = config.Default.Provider
	}
	if modelID == "" {
		modelID = config.Default.Model
	}
	if _, ok := config.FindProvider(providerName); !ok {
		return domain.Model{}, fmt.Errorf("proveedor %q no configurado", providerName)
	}
	// El listado de modelos es informativo, no un candado: el usuario puede
	// referenciar cualquier modelo que su cuenta soporte (los proveedores
	// cambian de catálogo constantemente). "*" habilita explícitamente todos.
	return domain.Model{Provider: providerName, ID: modelID}, nil
}
