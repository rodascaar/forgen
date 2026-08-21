package ports

import (
	"context"

	"github.com/rodascaar/forgen/internal/core/domain"
)

// SessionStore persiste sesiones en un storage concreto (JSONL, SQLite, ...).
type SessionStore interface {
	// Save persiste una sesión completa (crear o actualizar).
	Save(ctx context.Context, session domain.Session) error
	// Load recupera una sesión por ID.
	Load(ctx context.Context, id string) (domain.Session, error)
	// List devuelve las sesiones disponibles, ordenadas por actualización desc.
	List(ctx context.Context, limit int) ([]domain.Session, error)
	// Delete elimina una sesión por ID.
	Delete(ctx context.Context, id string) error
	// Export devuelve la representación portable de una sesión.
	Export(ctx context.Context, id string) ([]byte, error)
	// Import reconstruye una sesión desde su representación portable.
	Import(ctx context.Context, data []byte) (domain.Session, error)
}

// ConfigStore lee y escribe la configuración de la aplicación.
type ConfigStore interface {
	// Load lee la configuración desde la fuente de persistencia.
	Load(ctx context.Context) (domain.AppConfig, error)
	// Save escribe la configuración.
	Save(ctx context.Context, config domain.AppConfig) error
	// Path devuelve la ruta del archivo de configuración.
	Path() string
}

// PermissionStore persiste las reglas de permiso definidas por el usuario.
type PermissionStore interface {
	Load(ctx context.Context) ([]domain.PermissionRule, error)
	Save(ctx context.Context, rules []domain.PermissionRule) error
}
