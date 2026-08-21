package llm

import (
	"fmt"
	"log/slog"

	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// Factory crea el LLMProvider correcto según la configuración del proveedor.
type Factory struct {
	logger *slog.Logger
}

// NewFactory construye el factory de proveedores LLM.
func NewFactory(logger *slog.Logger) *Factory {
	return &Factory{logger: logger}
}

// Create construye el provider para una configuración de proveedor.
// La key se resuelve desde la variable de entorno configurada.
func (f *Factory) Create(config domain.ProviderConfig, getenv func(string) string) (ports.LLMProvider, error) {
	return f.CreateWithTags(config, getenv, nil)
}

// CreateWithTags construye el provider añadiendo headers extra a cada petición
// (para atribución de fase/modelo en el gateway).
func (f *Factory) CreateWithTags(config domain.ProviderConfig, getenv func(string) string, tags map[string]string) (ports.LLMProvider, error) {
	apiKey := config.ResolveAPIKey(getenv)
	client := NewClient(config.BaseURL, apiKey, f.logger)
	for key, value := range tags {
		client.ExtraHeaders[key] = value
	}

	switch config.Type {
	case domain.ProviderTypeAnthropic:
		return NewAnthropic(config.Name, client), nil
	case domain.ProviderTypeKimchi:
		return NewKimchi(config.Name, client), nil
	case domain.ProviderTypeOpenAICompatible:
		return NewOpenAICompatible(config.Name, client), nil
	default:
		return nil, fmt.Errorf("tipo de proveedor no soportado: %q", config.Type)
	}
}
