package ports

import (
	"context"

	"github.com/forgen/forgen/internal/core/domain"
)

// FermentEvent es un evento append-only de auditoría del estado.
type FermentEvent struct {
	Type      string
	FermentID string
	PrevHash  string
	Hash      string
	Data      map[string]any
}

// FermentStore persiste ferments con snapshot + log de eventos.
type FermentStore interface {
	// SaveSnapshot escribe el estado completo de forma atómica.
	SaveSnapshot(ctx context.Context, ferment domain.Ferment) error
	// LoadSnapshot carga el estado completo por ID.
	LoadSnapshot(ctx context.Context, id string) (domain.Ferment, error)
	// AppendEvent añade un evento al log de auditoría.
	AppendEvent(ctx context.Context, event FermentEvent) error
	// ListSnapshotIDs devuelve los ferments disponibles (snapshot ligero).
	List(ctx context.Context) ([]domain.Ferment, error)
	// Delete elimina un ferment y su log.
	Delete(ctx context.Context, id string) error
}
