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

type JSONLTodoStore struct {
	mu       sync.RWMutex
	filePath string
}

func NewJSONLTodoStore(filePath string) (*JSONLTodoStore, error) {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	return &JSONLTodoStore{filePath: filePath}, nil
}

func (s *JSONLTodoStore) Close() error { return nil }

func (s *JSONLTodoStore) Load(ctx context.Context, id string) (*domain.TodoList, error) {
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
		var list domain.TodoList
		if err := dec.Decode(&list); err != nil {
			break
		}
		if list.ID == id {
			return &list, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (s *JSONLTodoStore) Save(ctx context.Context, list *domain.TodoList) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	lists, err := s.loadAll()
	if err != nil {
		return err
	}
	found := false
	for i, l := range lists {
		if l.ID == list.ID {
			lists[i] = list
			found = true
			break
		}
	}
	if !found {
		lists = append(lists, list)
	}
	return s.writeAll(lists)
}

func (s *JSONLTodoStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	lists, err := s.loadAll()
	if err != nil {
		return err
	}
	found := false
	newLists := make([]*domain.TodoList, 0, len(lists))
	for _, l := range lists {
		if l.ID == id {
			found = true
			continue
		}
		newLists = append(newLists, l)
	}
	if !found {
		return domain.ErrNotFound
	}
	return s.writeAll(newLists)
}

func (s *JSONLTodoStore) List(ctx context.Context) ([]*domain.TodoList, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.loadAll()
}

func (s *JSONLTodoStore) loadAll() ([]*domain.TodoList, error) {
	file, err := os.Open(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []*domain.TodoList{}, nil
		}
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var lists []*domain.TodoList
	dec := json.NewDecoder(file)
	for {
		var list domain.TodoList
		if err := dec.Decode(&list); err != nil {
			break
		}
		lists = append(lists, &list)
	}
	return lists, nil
}

func (s *JSONLTodoStore) writeAll(lists []*domain.TodoList) error {
	tmp := s.filePath + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(file)
	for _, l := range lists {
		if err := enc.Encode(l); err != nil {
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

var _ ports.TodoStore = (*JSONLTodoStore)(nil)
