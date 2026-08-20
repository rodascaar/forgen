package storage

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/forgen/forgen/internal/core/domain"
	"github.com/forgen/forgen/internal/core/ports"
)

const (
	fermentSnapshotExt = ".json"
	fermentEventExt    = ".events.jsonl"
)

// JSONLFermentStore persiste ferments: snapshot atómico + log de eventos
// append-only con hash encadenado.
type JSONLFermentStore struct {
	dir string
}

// NewJSONLFermentStore crea el store y garantiza el directorio.
func NewJSONLFermentStore(dir string) (*JSONLFermentStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("crear directorio de ferments %s: %w", dir, err)
	}
	return &JSONLFermentStore{dir: dir}, nil
}

func (s *JSONLFermentStore) snapshotPath(id string) string {
	return filepath.Join(s.dir, id+fermentSnapshotExt)
}

func (s *JSONLFermentStore) eventPath(id string) string {
	return filepath.Join(s.dir, id+fermentEventExt)
}

// SaveSnapshot implementa ports.FermentStore (escritura atómica).
func (s *JSONLFermentStore) SaveSnapshot(_ context.Context, ferment domain.Ferment) error {
	data, err := json.MarshalIndent(ferment, "", "  ")
	if err != nil {
		return err
	}
	temp := s.snapshotPath(ferment.ID) + ".tmp"
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temp, s.snapshotPath(ferment.ID))
}

// LoadSnapshot implementa ports.FermentStore.
func (s *JSONLFermentStore) LoadSnapshot(_ context.Context, id string) (domain.Ferment, error) {
	data, err := os.ReadFile(s.snapshotPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return domain.Ferment{}, fmt.Errorf("ferment %q no encontrado", id)
		}
		return domain.Ferment{}, err
	}
	var ferment domain.Ferment
	if err := json.Unmarshal(data, &ferment); err != nil {
		return domain.Ferment{}, fmt.Errorf("snapshot de ferment %q corrupto: %w", id, err)
	}
	return ferment, nil
}

// AppendEvent implementa ports.FermentStore con hash encadenado.
func (s *JSONLFermentStore) AppendEvent(_ context.Context, event ports.FermentEvent) error {
	prevHash := s.lastHash(event.FermentID)

	// Encadenar: el hash cubre prevHash + payload del evento.
	record := struct {
		Type      string         `json:"type"`
		FermentID string         `json:"ferment_id"`
		PrevHash  string         `json:"prev_hash"`
		Hash      string         `json:"hash"`
		Data      map[string]any `json:"data"`
	}{
		Type:      event.Type,
		FermentID: event.FermentID,
		PrevHash:  prevHash,
		Data:      event.Data,
	}
	payload, _ := json.Marshal(record)
	sum := sha256.Sum256(payload)
	record.Hash = hex.EncodeToString(sum[:])

	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(s.eventPath(event.FermentID), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

// lastHash devuelve el hash del último evento del log (cadena vacía si no hay).
func (s *JSONLFermentStore) lastHash(id string) string {
	file, err := os.Open(s.eventPath(id))
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	var lastHash string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record struct {
			Hash string `json:"hash"`
		}
		if json.Unmarshal([]byte(line), &record) == nil {
			lastHash = record.Hash
		}
	}
	return lastHash
}

// List implementa ports.FermentStore.
func (s *JSONLFermentStore) List(_ context.Context) ([]domain.Ferment, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var ferments []domain.Ferment
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), fermentSnapshotExt) {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), fermentSnapshotExt)
		ferment, err := s.LoadSnapshot(context.Background(), id)
		if err != nil {
			continue
		}
		ferments = append(ferments, ferment)
	}
	return ferments, nil
}

// Delete implementa ports.FermentStore.
func (s *JSONLFermentStore) Delete(_ context.Context, id string) error {
	if _, err := os.Stat(s.snapshotPath(id)); os.IsNotExist(err) {
		return fmt.Errorf("ferment %q no encontrado", id)
	}
	if err := os.Remove(s.snapshotPath(id)); err != nil {
		return err
	}
	// El log de eventos puede no existir si no hubo mutaciones; ignorar.
	_ = os.Remove(s.eventPath(id))
	return nil
}

var _ ports.FermentStore = (*JSONLFermentStore)(nil)
