package todo

import (
	"context"
	"errors"
	"fmt"

	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

type Service struct{ store ports.TodoStore }

func NewService(store ports.TodoStore) *Service { return &Service{store: store} }

var ErrNotFound = errors.New("tarea no encontrada")

func (s *Service) CreateList(ctx context.Context, name string) (*domain.TodoList, error) {
	if name == "" {
		return nil, errors.New("el nombre no puede estar vacío")
	}
	list := domain.NewTodoList(name)
	if err := s.store.Save(ctx, list); err != nil {
		return nil, fmt.Errorf("guardar lista: %w", err)
	}
	return list, nil
}
func (s *Service) GetList(ctx context.Context, id string) (*domain.TodoList, error) {
	list, err := s.store.Load(ctx, id)
	if err != nil {
		return nil, ErrNotFound
	}
	return list, nil
}
func (s *Service) DeleteList(ctx context.Context, id string) error { return s.store.Delete(ctx, id) }
func (s *Service) ListLists(ctx context.Context) ([]*domain.TodoList, error) { return s.store.List(ctx) }
func (s *Service) AddTodo(ctx context.Context, listID, content, activeForm string) (*domain.Todo, error) {
	list, err := s.store.Load(ctx, listID)
	if err != nil {
		return nil, ErrNotFound
	}
	todo := domain.NewTodo(content, activeForm)
	list.AddTodo(todo)
	if err := s.store.Save(ctx, list); err != nil {
		return nil, err
	}
	return todo, nil
}
func (s *Service) UpdateTodo(ctx context.Context, listID, todoID, content, activeForm string) error {
	list, err := s.store.Load(ctx, listID)
	if err != nil {
		return ErrNotFound
	}
	todo := list.GetTodo(todoID)
	if todo == nil {
		return ErrNotFound
	}
	todo.UpdateContent(content, activeForm)
	return s.store.Save(ctx, list)
}
func (s *Service) DeleteTodo(ctx context.Context, listID, todoID string) error {
	list, err := s.store.Load(ctx, listID)
	if err != nil {
		return ErrNotFound
	}
	if !list.RemoveTodo(todoID) {
		return ErrNotFound
	}
	return s.store.Save(ctx, list)
}
func (s *Service) MoveTodo(ctx context.Context, listID, todoID string, newIndex int) error {
	list, err := s.store.Load(ctx, listID)
	if err != nil {
		return ErrNotFound
	}
	if !list.MoveTodo(todoID, newIndex) {
		return ErrNotFound
	}
	return s.store.Save(ctx, list)
}
func (s *Service) UpdateTodoStatus(ctx context.Context, listID, todoID string, status domain.TodoStatus) error {
	list, err := s.store.Load(ctx, listID)
	if err != nil {
		return ErrNotFound
	}
	todo := list.GetTodo(todoID)
	if todo == nil {
		return ErrNotFound
	}
	switch status {
	case domain.TodoStatusPending:
		todo.MarkPending()
	case domain.TodoStatusInProgress:
		todo.MarkInProgress()
	case domain.TodoStatusDone:
		todo.MarkDone()
	case domain.TodoStatusCancelled:
		todo.MarkCancelled()
	default:
		return errors.New("estado inválido")
	}
	return s.store.Save(ctx, list)
}
func (s *Service) GetTodoProgress(ctx context.Context, listID string) (int, int, float64, error) {
	list, err := s.store.Load(ctx, listID)
	if err != nil {
		return 0, 0, 0, ErrNotFound
	}
	d, t := list.Progress()
	return d, t, list.ProgressPercent(), nil
}
