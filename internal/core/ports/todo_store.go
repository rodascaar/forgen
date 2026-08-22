package ports

import (
	"context"

	"github.com/rodascaar/forgen/internal/core/domain"
)

type TodoStore interface {
	Load(ctx context.Context, id string) (*domain.TodoList, error)
	Save(ctx context.Context, list *domain.TodoList) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]*domain.TodoList, error)
}
