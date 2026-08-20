package storage

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/forgen/forgen/internal/core/domain"
	"github.com/forgen/forgen/internal/core/ports"
)

// JSONLUsageStore persiste registros de uso en un log append-only.
type JSONLUsageStore struct {
	path string
}

// NewJSONLUsageStore crea el store de uso.
func NewJSONLUsageStore(path string) *JSONLUsageStore {
	return &JSONLUsageStore{path: path}
}

// Append implementa ports.UsageStore.
func (s *JSONLUsageStore) Append(_ context.Context, record domain.UsageRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

// List implementa ports.UsageStore (devuelve los N últimos en orden desc).
func (s *JSONLUsageStore) List(_ context.Context, limit int) ([]domain.UsageRecord, error) {
	file, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var records []domain.UsageRecord
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var record domain.UsageRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("leer uso: %w", err)
	}

	// Invertir para que el más reciente esté primero.
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}
	if limit > 0 && len(records) > limit {
		records = records[:limit]
	}
	return records, nil
}

var _ ports.UsageStore = (*JSONLUsageStore)(nil)
