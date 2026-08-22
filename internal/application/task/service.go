package task

import (
	"context"
	"errors"
	"fmt"

	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

type Service struct {
	executor ports.TaskExecutor
	store    ports.TaskStore
}

func NewService(executor ports.TaskExecutor, store ports.TaskStore) *Service {
	return &Service{executor: executor, store: store}
}

var ErrNotFound = errors.New("tarea no encontrada")

func (s *Service) CreateTask(ctx context.Context, taskType domain.TaskType, name, description string, config domain.SubAgentConfig) (*domain.Task, error) {
	if name == "" {
		return nil, errors.New("el nombre no puede estar vacío")
	}
	if description == "" {
		return nil, errors.New("la descripción no puede estar vacía")
	}
	task := domain.NewTask(taskType, name, description, config)
	if err := s.store.Save(ctx, task); err != nil {
		return nil, fmt.Errorf("guardar tarea: %w", err)
	}
	return task, nil
}
func (s *Service) GetTask(ctx context.Context, id string) (*domain.Task, error) {
	t, err := s.store.Load(ctx, id)
	if err != nil {
		return nil, ErrNotFound
	}
	return t, nil
}
func (s *Service) ExecuteTask(ctx context.Context, taskID string) (*domain.TaskResult, error) {
	t, err := s.store.Load(ctx, taskID)
	if err != nil {
		return nil, ErrNotFound
	}
	if t.Status == domain.TaskStatusRunning {
		return nil, errors.New("la tarea ya se está ejecutando")
	}
	if t.IsTerminal() {
		return nil, fmt.Errorf("la tarea ya terminó: %s", t.Status)
	}
	return s.executor.Execute(ctx, t)
}
func (s *Service) ExecuteTaskAsync(ctx context.Context, taskID string) error {
	t, err := s.store.Load(ctx, taskID)
	if err != nil {
		return ErrNotFound
	}
	if t.Status == domain.TaskStatusRunning {
		return errors.New("la tarea ya se está ejecutando")
	}
	if t.IsTerminal() {
		return fmt.Errorf("la tarea ya terminó: %s", t.Status)
	}
	go func() { _, _ = s.executor.Execute(context.WithoutCancel(ctx), t) }()
	return nil
}
func (s *Service) CancelTask(ctx context.Context, taskID string) error { return s.executor.Cancel(ctx, taskID) }
func (s *Service) GetTaskStatus(ctx context.Context, taskID string) (domain.TaskStatus, error) {
	return s.executor.GetStatus(ctx, taskID)
}
func (s *Service) ListTasks(ctx context.Context, parentID *string) ([]*domain.Task, error) {
	return s.executor.ListTasks(ctx, parentID)
}
func (s *Service) DeleteTask(ctx context.Context, id string) error { return s.store.Delete(ctx, id) }
