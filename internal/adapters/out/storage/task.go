package storage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

type JSONLTaskStore struct {
	mu       sync.RWMutex
	filePath string
}

func NewJSONLTaskStore(filePath string) (*JSONLTaskStore, error) {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return nil, err
	}
	return &JSONLTaskStore{filePath: filePath}, nil
}

func (s *JSONLTaskStore) Close() error { return nil }

func (s *JSONLTaskStore) Save(ctx context.Context, task *domain.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks, err := s.loadAll()
	if err != nil {
		return err
	}
	found := false
	for i, t := range tasks {
		if t.ID == task.ID {
			tasks[i] = task
			found = true
			break
		}
	}
	if !found {
		tasks = append(tasks, task)
	}
	return s.writeAll(tasks)
}

func (s *JSONLTaskStore) Load(ctx context.Context, id string) (*domain.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	file, err := os.Open(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	defer func() { _ = file.Close() }()
	dec := json.NewDecoder(file)
	for {
		var task domain.Task
		if err := dec.Decode(&task); err != nil {
			break
		}
		if task.ID == id {
			return &task, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *JSONLTaskStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tasks, err := s.loadAll()
	if err != nil {
		return err
	}
	found := false
	newTasks := make([]*domain.Task, 0, len(tasks))
	for _, t := range tasks {
		if t.ID == id {
			found = true
			continue
		}
		newTasks = append(newTasks, t)
	}
	if !found {
		return domain.ErrNotFound
	}
	return s.writeAll(newTasks)
}

func (s *JSONLTaskStore) List(ctx context.Context, parentID *string) ([]*domain.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tasks, err := s.loadAll()
	if err != nil {
		return nil, err
	}
	if parentID == nil {
		return tasks, nil
	}
	var filtered []*domain.Task
	for _, t := range tasks {
		if t.ParentID != nil && *t.ParentID == *parentID {
			filtered = append(filtered, t)
		}
	}
	return filtered, nil
}

func (s *JSONLTaskStore) loadAll() ([]*domain.Task, error) {
	file, err := os.Open(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []*domain.Task{}, nil
		}
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var tasks []*domain.Task
	dec := json.NewDecoder(file)
	for {
		var task domain.Task
		if err := dec.Decode(&task); err != nil {
			break
		}
		tasks = append(tasks, &task)
	}
	return tasks, nil
}

func (s *JSONLTaskStore) writeAll(tasks []*domain.Task) error {
	tmp := s.filePath + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(file)
	for _, t := range tasks {
		if err := enc.Encode(t); err != nil {
			_ = file.Close()
			_ = os.Remove(tmp)
			return err
		}
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, s.filePath)
}

var _ ports.TaskStore = (*JSONLTaskStore)(nil)
