package ports

import (
	"context"

	"github.com/rodascaar/forgen/internal/core/domain"
)

// CheckpointStore persiste snapshots del workspace para permitir rollback de
// iteraciones fallidas del agente (rollback interno, sin depender de Git).
type CheckpointStore interface {
	// Create toma un snapshot del workspace bajo la sesión indicada y devuelve
	// el checkpoint creado.
	Create(ctx context.Context, workspace, sessionID string) (domain.Checkpoint, error)
	// Restore revierte el workspace al estado de un checkpoint.
	Restore(ctx context.Context, id string) error
	// List devuelve los checkpoints de una sesión, del más reciente al más viejo.
	List(ctx context.Context, sessionID string, limit int) ([]domain.Checkpoint, error)
	// Prune elimina los checkpoints más antiguos de cada sesión dejando solo
	// los `keep` más recientes.
	Prune(ctx context.Context, keep int) error
}
