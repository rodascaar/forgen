// Package credentials implementa ports.CredentialStore con prioridad al
// almacén seguro del sistema operativo (Keychain / Secret Service / Credential
// Manager) y degradación a un archivo local 0600 cuando no está disponible.
package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rodascaar/forgen/internal/core/ports"
	"github.com/zalando/go-keyring"
)

// serviceName identifica el servicio en el almacén del sistema operativo.
const serviceName = "forgen"

// probeKey es una clave usada para detectar si el keyring está disponible.
const probeKey = "__forgen_probe__"

// Store guarda secretos en el almacén del SO o en un archivo de respaldo.
type Store struct {
	service  string
	filePath string
	useFile  bool
	mu       sync.Mutex
}

// NewStore construye el store. Detecta una vez si el keyring del SO está
// disponible; si no, usa el archivo local 0600 como respaldo.
//
// Se puede forzar el respaldo con FORGEN_KEYRING=file (útil para CI o
// entornos headless).
func NewStore(fallbackPath string) *Store {
	s := &Store{service: serviceName, filePath: fallbackPath}

	if strings.EqualFold(os.Getenv("FORGEN_KEYRING"), "file") {
		s.useFile = true
		return s
	}

	// Sonda: si el keyring responde con "no encontrado" para una clave inexistente,
	// significa que está operativo. Cualquier otro error (sin dbus, etc.) → respaldo.
	if _, err := keyring.Get(serviceName, probeKey); err != nil {
		if !errors.Is(err, keyring.ErrNotFound) {
			s.useFile = true
		}
	}
	return s
}

// Set implementa ports.CredentialStore.
func (s *Store) Set(_ context.Context, key, secret string) error {
	if s.useFile {
		return s.fileSet(key, secret)
	}
	return keyring.Set(s.service, key, secret)
}

// Get implementa ports.CredentialStore.
func (s *Store) Get(_ context.Context, key string) (string, error) {
	if s.useFile {
		return s.fileGet(key)
	}
	secret, err := keyring.Get(s.service, key)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", fmt.Errorf("credencial %q no encontrada", key)
		}
		return "", fmt.Errorf("leer credencial %q: %w", key, err)
	}
	return secret, nil
}

// Delete implementa ports.CredentialStore.
func (s *Store) Delete(_ context.Context, key string) error {
	if s.useFile {
		return s.fileDelete(key)
	}
	if err := keyring.Delete(s.service, key); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("eliminar credencial %q: %w", key, err)
	}
	return nil
}

// --- respaldo local (archivo 0600) ---

type filePayload struct {
	Secrets map[string]string `json:"secrets"`
}

func (s *Store) fileSet(key, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := s.loadFile()
	if err != nil {
		return err
	}
	payload.Secrets[key] = secret
	return s.saveFile(payload)
}

func (s *Store) fileGet(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := s.loadFile()
	if err != nil {
		return "", err
	}
	secret, ok := payload.Secrets[key]
	if !ok {
		return "", fmt.Errorf("credencial %q no encontrada", key)
	}
	return secret, nil
}

func (s *Store) fileDelete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	payload, err := s.loadFile()
	if err != nil {
		return err
	}
	delete(payload.Secrets, key)
	return s.saveFile(payload)
}

func (s *Store) loadFile() (filePayload, error) {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return filePayload{Secrets: map[string]string{}}, nil
		}
		return filePayload{}, fmt.Errorf("leer credenciales: %w", err)
	}
	var payload filePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return filePayload{}, fmt.Errorf("parsear credenciales: %w", err)
	}
	if payload.Secrets == nil {
		payload.Secrets = map[string]string{}
	}
	return payload, nil
}

func (s *Store) saveFile(payload filePayload) error {
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0o600)
}

var _ ports.CredentialStore = (*Store)(nil)
