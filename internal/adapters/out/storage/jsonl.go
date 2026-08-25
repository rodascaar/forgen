// Package storage implementa los puertos de persistencia sobre disco.
package storage

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// sessionMeta es la cabecera del archivo de sesión.
type sessionMeta struct {
	Type              string    `json:"type"`
	ID                string    `json:"id"`
	Workspace         string    `json:"workspace"`
	Provider          string    `json:"provider"`
	Model             string    `json:"model"`
	Agent             string    `json:"agent"`
	StartedAt         time.Time `json:"started_at"`
	Summary           string    `json:"summary"`
	CompactBoundary   int       `json:"compact_boundary,omitempty"`
	CompactionCount   int       `json:"compaction_count,omitempty"`
	CompactionSummary string    `json:"compaction_summary,omitempty"`
}

type messageRecord struct {
	Type        string              `json:"type"`
	Role        string              `json:"role"`
	Content     []contentPartRecord `json:"content"`
	ToolCallID  string              `json:"tool_call_id,omitempty"`
	ToolName    string              `json:"tool_name,omitempty"`
	CreatedAt   time.Time           `json:"created_at"`
	CompactedAt *time.Time          `json:"compacted_at,omitempty"`
	IsSummary   bool                `json:"is_summary,omitempty"`
}

type contentPartRecord struct {
	Type string          `json:"type"`
	Text string          `json:"text,omitempty"`
	Call *toolCallRecord `json:"call,omitempty"`
}

type toolCallRecord struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

const (
	metaRecordType    = "meta"
	messageRecordType = "message"
	sessionFileExt    = ".jsonl"
)

// JSONLStore persiste sesiones como archivos append-only por sesión.
// JSONLStore persiste sesiones como archivos append-only por sesión.
// JSONLStore persiste sesiones como archivos append-only por sesión.
// JSONLStore persiste sesiones como archivos append-only por sesión.
// JSONLStore persiste sesiones como archivos append-only por sesión.
type JSONLStore struct {
	dir    string
	mu     sync.Mutex
	closed bool
}

// NewJSONLStore crea el store y garantiza que el directorio exista.
func NewJSONLStore(dir string) (*JSONLStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("crear directorio de sesiones %s: %w", dir, err)
	}
	return &JSONLStore{dir: dir}, nil
}

// Save implementa ports.SessionStore.
func (s *JSONLStore) Save(_ context.Context, session domain.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("store is closed")
	}
	path := s.filePath(session.ID)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return s.create(session, path)
	}
	return s.rewrite(session, path)
}

// Close cierra el store y libera recursos.
func (s *JSONLStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}
func (s *JSONLStore) filePath(id string) string {
	return filepath.Join(s.dir, id+sessionFileExt)
}

func (s *JSONLStore) create(session domain.Session, path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if err := writeMeta(file, session); err != nil {
		return err
	}
	for _, message := range session.Messages {
		if err := writeMessage(file, message); err != nil {
			return err
		}
	}
	return nil
}

func (s *JSONLStore) rewrite(session domain.Session, path string) error {
	temp := path + ".tmp"
	file, err := os.OpenFile(temp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(temp)
	}()
	if err := writeMeta(file, session); err != nil {
		return err
	}
	for _, message := range session.Messages {
		if err := writeMessage(file, message); err != nil {
			return err
		}
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	// Renombrado atómico: la sesión nunca queda a medio escribir.
	return os.Rename(temp, path)
}

func writeMeta(file *os.File, session domain.Session) error {
	meta := sessionMeta{
		Type:              metaRecordType,
		ID:                session.ID,
		Workspace:         session.Workspace,
		Provider:          session.Model.Provider,
		Model:             session.Model.ID,
		Agent:             session.Agent,
		StartedAt:         session.StartedAt,
		Summary:           session.Summary(),
		CompactBoundary:   session.CompactBoundary,
		CompactionCount:   session.CompactionCount,
		CompactionSummary: session.CompactionSummary,
	}
	return writeJSONLine(file, meta)
}

func writeMessage(file *os.File, message domain.Message) error {
	record := messageRecord{
		Type:        messageRecordType,
		Role:        string(message.Role),
		ToolCallID:  message.ToolCallID,
		ToolName:    message.ToolName,
		CreatedAt:   message.CreatedAt,
		CompactedAt: message.CompactedAt,
		IsSummary:   message.IsSummary,
	}
	for _, part := range message.Content {
		content := contentPartRecord{Type: part.Type, Text: part.Text}
		if part.Call != nil {
			content.Call = &toolCallRecord{
				ID:        part.Call.ID,
				Name:      part.Call.Name,
				Arguments: part.Call.Arguments,
			}
		}
		record.Content = append(record.Content, content)
	}
	return writeJSONLine(file, record)
}

func writeJSONLine(file *os.File, record any) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = file.Write(append(data, '\n'))
	return err
}

// Load implementa ports.SessionStore.
func (s *JSONLStore) Load(_ context.Context, id string) (domain.Session, error) {
	path := s.filePath(id)
	file, err := os.Open(path)
	if err != nil {
		return domain.Session{}, fmt.Errorf("abrir sesión %s: %w", id, err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var session domain.Session
	lineIndex := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if lineIndex == 0 {
			var meta sessionMeta
			if err := json.Unmarshal(line, &meta); err != nil {
				return domain.Session{}, fmt.Errorf("cabecera de sesión %s corrupta: %w", id, err)
			}
			session = domain.Session{
				ID:                meta.ID,
				Workspace:         meta.Workspace,
				Model:             domain.Model{Provider: meta.Provider, ID: meta.Model},
				Agent:             meta.Agent,
				StartedAt:         meta.StartedAt,
				UpdatedAt:         meta.StartedAt,
				CompactBoundary:   meta.CompactBoundary,
				CompactionCount:   meta.CompactionCount,
				CompactionSummary: meta.CompactionSummary,
			}
			lineIndex++
			continue
		}
		var record messageRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return domain.Session{}, fmt.Errorf("mensaje %d de sesión %s corrupto: %w", lineIndex, id, err)
		}
		message := domain.Message{
			Role:        domain.Role(record.Role),
			ToolCallID:  record.ToolCallID,
			ToolName:    record.ToolName,
			CreatedAt:   record.CreatedAt,
			CompactedAt: record.CompactedAt,
			IsSummary:   record.IsSummary,
		}
		for _, part := range record.Content {
			contentPart := domain.ContentPart{Type: part.Type, Text: part.Text}
			if part.Call != nil {
				contentPart.Call = &domain.ToolCall{
					ID:        part.Call.ID,
					Name:      part.Call.Name,
					Arguments: part.Call.Arguments,
				}
			}
			message.Content = append(message.Content, contentPart)
		}
		session.Messages = append(session.Messages, message)
		lineIndex++
	}
	if err := scanner.Err(); err != nil {
		return domain.Session{}, fmt.Errorf("leer sesión %s: %w", id, err)
	}
	if session.ID == "" {
		return domain.Session{}, fmt.Errorf("sesión %s no encontrada", id)
	}
	if len(session.Messages) > 0 {
		session.UpdatedAt = session.Messages[len(session.Messages)-1].CreatedAt
	}
	return session, nil
}

// List implementa ports.SessionStore.
func (s *JSONLStore) List(_ context.Context, limit int) ([]domain.Session, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	sessions := make([]domain.Session, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), sessionFileExt) {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), sessionFileExt)
		meta, err := s.readMeta(id)
		if err != nil {
			continue // ignorar archivos corruptos en el listado
		}
		sessions = append(sessions, domain.Session{
			ID:                meta.ID,
			Workspace:         meta.Workspace,
			Model:             domain.Model{Provider: meta.Provider, ID: meta.Model},
			Agent:             meta.Agent,
			StartedAt:         meta.StartedAt,
			UpdatedAt:         meta.StartedAt,
			SummaryCache:      meta.Summary,
			CompactBoundary:   meta.CompactBoundary,
			CompactionCount:   meta.CompactionCount,
			CompactionSummary: meta.CompactionSummary,
		})
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].StartedAt.After(sessions[j].StartedAt) })
	if limit > 0 && len(sessions) > limit {
		sessions = sessions[:limit]
	}
	return sessions, nil
}

func (s *JSONLStore) readMeta(id string) (sessionMeta, error) {
	file, err := os.Open(s.filePath(id))
	if err != nil {
		return sessionMeta{}, err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	if !scanner.Scan() {
		return sessionMeta{}, errors.New("archivo de sesión vacío")
	}
	var meta sessionMeta
	if err := json.Unmarshal(scanner.Bytes(), &meta); err != nil {
		return sessionMeta{}, err
	}
	return meta, nil
}

// Delete implementa ports.SessionStore.
func (s *JSONLStore) Delete(_ context.Context, id string) error {
	err := os.Remove(s.filePath(id))
	if os.IsNotExist(err) {
		return fmt.Errorf("sesión %s no encontrada", id)
	}
	return err
}

// Export implementa ports.SessionStore (devuelve el JSONL crudo portable).
func (s *JSONLStore) Export(_ context.Context, id string) ([]byte, error) {
	return os.ReadFile(s.filePath(id))
}

// Import implementa ports.SessionStore (reconstruye desde JSONL portable).
func (s *JSONLStore) Import(_ context.Context, data []byte) (domain.Session, error) {
	// Parsear en memoria con la misma lógica que Load.
	var session domain.Session
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	for index, line := range lines {
		if len(line) == 0 {
			continue
		}
		if index == 0 {
			var meta sessionMeta
			if err := json.Unmarshal(line, &meta); err != nil {
				return domain.Session{}, fmt.Errorf("cabecera de sesión corrupta: %w", err)
			}
			session = domain.Session{
				ID:                meta.ID,
				Workspace:         meta.Workspace,
				Model:             domain.Model{Provider: meta.Provider, ID: meta.Model},
				Agent:             meta.Agent,
				StartedAt:         meta.StartedAt,
				UpdatedAt:         meta.StartedAt,
				CompactBoundary:   meta.CompactBoundary,
				CompactionCount:   meta.CompactionCount,
				CompactionSummary: meta.CompactionSummary,
			}
			continue
		}
		var record messageRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return domain.Session{}, fmt.Errorf("mensaje corrupto: %w", err)
		}
		message := domain.Message{
			Role:        domain.Role(record.Role),
			ToolCallID:  record.ToolCallID,
			ToolName:    record.ToolName,
			CreatedAt:   record.CreatedAt,
			CompactedAt: record.CompactedAt,
			IsSummary:   record.IsSummary,
		}
		for _, part := range record.Content {
			contentPart := domain.ContentPart{Type: part.Type, Text: part.Text}
			if part.Call != nil {
				contentPart.Call = &domain.ToolCall{ID: part.Call.ID, Name: part.Call.Name, Arguments: part.Call.Arguments}
			}
			message.Content = append(message.Content, contentPart)
		}
		session.Messages = append(session.Messages, message)
	}
	if session.ID == "" {
		return domain.Session{}, errors.New("sesión vacía o sin cabecera")
	}
	if err := s.Save(context.Background(), session); err != nil {
		return domain.Session{}, err
	}
	return session, nil
}

var _ ports.SessionStore = (*JSONLStore)(nil)
