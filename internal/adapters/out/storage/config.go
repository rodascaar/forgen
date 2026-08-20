package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/forgen/forgen/internal/core/domain"
	"github.com/forgen/forgen/internal/core/ports"
	"gopkg.in/yaml.v3"
)

// YAMLConfigStore persiste la configuración en un archivo YAML.
type YAMLConfigStore struct {
	path string
}

// NewYAMLConfigStore crea el store de configuración.
func NewYAMLConfigStore(path string) *YAMLConfigStore {
	return &YAMLConfigStore{path: path}
}

// Load implementa ports.ConfigStore.
func (s *YAMLConfigStore) Load(_ context.Context) (domain.AppConfig, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Sin archivo: devolver configuración por defecto (no es un error).
			return domain.DefaultAppConfig(), nil
		}
		return domain.AppConfig{}, fmt.Errorf("leer config %s: %w", s.path, err)
	}
	var config domain.AppConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return domain.AppConfig{}, fmt.Errorf("parsear config %s: %w", s.path, err)
	}
	return config, nil
}

// Save implementa ports.ConfigStore.
func (s *YAMLConfigStore) Save(_ context.Context, config domain.AppConfig) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	// Escribir con permisos restrictivos: la config puede contener tokens.
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("escribir config %s: %w", s.path, err)
	}
	return nil
}

// Path implementa ports.ConfigStore.
func (s *YAMLConfigStore) Path() string { return s.path }

var _ ports.ConfigStore = (*YAMLConfigStore)(nil)

// JSONPermissionStore persiste las reglas de permiso del usuario.
type JSONPermissionStore struct {
	path string
}

// NewJSONPermissionStore crea el store de reglas de permiso.
func NewJSONPermissionStore(path string) *JSONPermissionStore {
	return &JSONPermissionStore{path: path}
}

// Load implementa ports.PermissionStore.
func (s *JSONPermissionStore) Load(_ context.Context) ([]domain.PermissionRule, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("leer reglas de permiso: %w", err)
	}
	var rules []domain.PermissionRule
	if err := yaml.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("parsear reglas de permiso: %w", err)
	}
	return rules, nil
}

// Save implementa ports.PermissionStore.
func (s *JSONPermissionStore) Save(_ context.Context, rules []domain.PermissionRule) error {
	data, err := yaml.Marshal(rules)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("escribir reglas de permiso: %w", err)
	}
	return nil
}

var _ ports.PermissionStore = (*JSONPermissionStore)(nil)
