package ports

import (
	"context"

	"github.com/rodascaar/forgen/internal/core/domain"
)

// UsageRecorder registra el consumo de tokens de las llamadas al modelo.
type UsageRecorder interface {
	// Record añade un registro de uso.
	Record(ctx context.Context, record domain.UsageRecord) error
}

// UsageStore persiste y consulta los registros de uso.
type UsageStore interface {
	// Append añade un registro (append-only).
	Append(ctx context.Context, record domain.UsageRecord) error
	// List devuelve los registros más recientes.
	List(ctx context.Context, limit int) ([]domain.UsageRecord, error)
}
