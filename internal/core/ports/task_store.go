package ports

import (
	"context"

	"github.com/rodascaar/forgen/internal/core/domain"
)

type TaskStore interface {
	Save(ctx context.Context, task *domain.Task) error
	Load(ctx context.Context, id string) (*domain.Task, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, parentID *string) ([]*domain.Task, error)
}

type TaskExecutor interface {
	Execute(ctx context.Context, task *domain.Task) (*domain.TaskResult, error)
	ExecuteWithConfig(ctx context.Context, task *domain.Task, config domain.SubAgentConfig) (*domain.TaskResult, error)
	Cancel(ctx context.Context, taskID string) error
	GetStatus(ctx context.Context, taskID string) (domain.TaskStatus, error)
	ListTasks(ctx context.Context, parentID *string) ([]*domain.Task, error)
}
