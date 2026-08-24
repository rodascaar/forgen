// Package session contiene el caso de uso de gestión de sesiones.
package session

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// Service gestiona el ciclo de vida de las sesiones.
type Service struct {
	store ports.SessionStore
	now   func() time.Time
}

// NewService construye el servicio de sesiones.
func NewService(store ports.SessionStore) *Service {
	return &Service{store: store, now: time.Now}
}

// Create inicia una sesión nueva para un workspace.
func (s *Service) Create(ctx context.Context, workspace string, model domain.Model, agent string) (domain.Session, error) {
	session := domain.Session{
		ID:        uuid.NewString(),
		Workspace: workspace,
		Model:     model,
		Agent:     agent,
		StartedAt: s.now(),
		UpdatedAt: s.now(),
	}
	if err := s.store.Save(ctx, session); err != nil {
		return domain.Session{}, fmt.Errorf("persistir sesión nueva: %w", err)
	}
	return session, nil
}

// Resume carga una sesión por ID.
func (s *Service) Resume(ctx context.Context, id string) (domain.Session, error) {
	session, err := s.store.Load(ctx, id)
	if err != nil {
		return domain.Session{}, fmt.Errorf("resumir sesión %q: %w", id, err)
	}
	return session, nil
}

// List lista las sesiones recientes.
func (s *Service) List(ctx context.Context, limit int) ([]domain.Session, error) {
	return s.store.List(ctx, limit)
}

// AppendMessage agrega un mensaje y persiste.
func (s *Service) AppendMessage(ctx context.Context, session domain.Session, message domain.Message) (domain.Session, error) {
	session.Messages = append(session.Messages, message)
	session.UpdatedAt = s.now()
	if err := s.store.Save(ctx, session); err != nil {
		return session, fmt.Errorf("persistir mensaje: %w", err)
	}
	return session, nil
}

// Save persiste directamente una sesión actualizada.
func (s *Service) Save(ctx context.Context, session domain.Session) error {
	session.UpdatedAt = s.now()
	if err := s.store.Save(ctx, session); err != nil {
		return fmt.Errorf("persistir sesión: %w", err)
	}
	return nil
}

// Delete elimina una sesión.
func (s *Service) Delete(ctx context.Context, id string) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return fmt.Errorf("eliminar sesión %q: %w", id, err)
	}
	return nil
}

// Export devuelve la representación portable de una sesión.
func (s *Service) Export(ctx context.Context, id string) ([]byte, error) {
	data, err := s.store.Export(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("exportar sesión %q: %w", id, err)
	}
	return data, nil
}

// Import reconstruye una sesión desde su representación portable.
func (s *Service) Import(ctx context.Context, data []byte) (domain.Session, error) {
	session, err := s.store.Import(ctx, data)
	if err != nil {
		return domain.Session{}, fmt.Errorf("importar sesión: %w", err)
	}
	return session, nil
}
