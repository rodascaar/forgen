package llm

import (
	"context"
	"fmt"

	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// Kimchi es el adapter del gateway gestionado de Kimchi.
// Usa el protocolo OpenAI-compatible y añade metadatos de fase y etiquetas.
type Kimchi struct {
	name   string
	client *Client
	openai *OpenAICompatible
}

// NewKimchi construye el adapter del gateway de Kimchi.
func NewKimchi(name string, client *Client) *Kimchi {
	return &Kimchi{
		name:   name,
		client: client,
		openai: NewOpenAICompatible(name, client),
	}
}

// Name implementa ports.LLMProvider.
func (k *Kimchi) Name() string { return k.name }

// StreamChat implementa ports.LLMProvider delegando en OpenAI-compatible.
func (k *Kimchi) StreamChat(ctx context.Context, request ports.ChatRequest, handler ports.StreamHandler) error {
	// El gateway espera el ID completo "provider/model" para el routing multi-modelo.
	request.Model = domain.Model{
		Provider: request.Model.Provider,
		ID:       fmt.Sprintf("%s/%s", request.Model.Provider, request.Model.ID),
	}
	return k.openai.StreamChat(ctx, request, handler)
}

var _ ports.LLMProvider = (*Kimchi)(nil)
