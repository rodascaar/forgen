package storage

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rodascaar/forgen/internal/core/domain"
)

// CheckpointStore persiste snapshots del workspace en disco.
type CheckpointStore struct {
	root string
}

// checkpointManifest describe el contenido de un snapshot.
type checkpointManifest struct {
	ID        string   `json:"id"`
	SessionID string   `json:"session_id"`
	Workspace string   `json:"workspace"`
	CreatedAt string   `json:"created_at"`
	Files     []string `json:"files"`
}

// ignoredDir devuelve true para directorios que no deben snapshotearse.
func ignoredDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".venv", "venv", "dist", "bin",
		".cache", ".idea", ".vscode", ".opencode", ".claude", "checkpoints":
		return true
	}
	return false
}

// NewCheckpointStore construye el store en un directorio raíz.
func NewCheckpointStore(root string) *CheckpointStore {
	return &CheckpointStore{root: root}
}

// isGitRepo detecta si workspace es un repo git.
func isGitRepo(workspace string) bool {
	if _, err := os.Stat(filepath.Join(workspace, ".git")); err == nil {
		return true
	}
	return false
}

// gitChangedFiles lista archivos modificados + untracked via git (incremental, opencode-style).
func gitChangedFiles(ctx context.Context, workspace string) []string {
	var out []string
	// git diff --name-only (staged + unstaged)
	cmd := exec.CommandContext(ctx, "git", "-C", workspace, "diff", "--name-only", "HEAD")
	if b, err := cmd.Output(); err == nil {
		for line := range strings.SplitSeq(strings.TrimSpace(string(b)), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				out = append(out, line)
			}
		}
	}
	// git diff --name-only (unstaged)
	cmd2 := exec.CommandContext(ctx, "git", "-C", workspace, "diff", "--name-only")
	if b, err := cmd2.Output(); err == nil {
		for line := range strings.SplitSeq(strings.TrimSpace(string(b)), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !contains(out, line) {
				out = append(out, line)
			}
		}
	}
	// untracked
	cmd3 := exec.CommandContext(ctx, "git", "-C", workspace, "ls-files", "--others", "--exclude-standard")
	if b, err := cmd3.Output(); err == nil {
		for line := range strings.SplitSeq(strings.TrimSpace(string(b)), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !contains(out, line) {
				out = append(out, line)
			}
		}
	}
	return out
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// Create toma un snapshot del workspace bajo la sesión indicada.
// Si es repo git, usa git diff incremental (rápido); si no, fallback a WalkDir full (forgen README: siempre funciona).
func (s *CheckpointStore) Create(ctx context.Context, workspace, sessionID string) (domain.Checkpoint, error) {
	id := time.Now().Format("20060102150405.000000000")
	dest := filepath.Join(s.root, sessionID, id)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return domain.Checkpoint{}, err
	}

	manifest := checkpointManifest{
		ID:        id,
		SessionID: sessionID,
		Workspace: workspace,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}

	var total int64
	// Git incremental path: solo archivos cambiados
	if isGitRepo(workspace) {
		changed := gitChangedFiles(ctx, workspace)
		if len(changed) > 0 {
			for _, rel := range changed {
				if strings.HasPrefix(rel, "..") {
					continue
				}
				src := filepath.Join(workspace, rel)
				info, err := os.Stat(src)
				if err != nil || info.IsDir() || info.Size() > 4<<20 {
					continue
				}
				data, err := os.ReadFile(src)
				if err != nil {
					continue
				}
				out := filepath.Join(dest, rel)
				if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
					continue
				}
				if err := os.WriteFile(out, data, 0o644); err != nil {
					continue
				}
				total += int64(len(data))
				manifest.Files = append(manifest.Files, rel)
			}
			// Si hay cambios git, ya terminamos (no WalkDir)
			if len(manifest.Files) > 0 {
				data, err := json.MarshalIndent(manifest, "", "  ")
				if err != nil {
					return domain.Checkpoint{}, err
				}
				if err := os.WriteFile(filepath.Join(dest, "manifest.json"), data, 0o600); err != nil {
					return domain.Checkpoint{}, err
				}
				return domain.Checkpoint{
					ID:         id,
					SessionID:  sessionID,
					Workspace:  workspace,
					CreatedAt:  time.Now(),
					FileCount:  len(manifest.Files),
					TotalBytes: total,
				}, nil
			}
		}
		// Si no hay cambios git o falló, fallback a WalkDir (mantiene compat)
	}
	_ = filepath.WalkDir(workspace, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if path != workspace && ignoredDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(workspace, path)
		if rerr != nil || strings.HasPrefix(rel, "..") {
			return nil
		}
		// Cargar el fichero completo (límite práctico de tamaño).
		info, ierr := d.Info()
		if ierr != nil || info.Size() > 4<<20 {
			return nil
		}
		src, oerr := os.Open(path)
		if oerr != nil {
			return nil
		}
		defer func() { _ = src.Close() }()

		out := filepath.Join(dest, rel)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return nil
		}
		dst, cerr := os.Create(out)
		if cerr != nil {
			return nil
		}
		n, cerr := io.Copy(dst, src)
		_ = dst.Close()
		if cerr != nil {
			return nil
		}
		total += n
		manifest.Files = append(manifest.Files, rel)
		return nil
	})

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return domain.Checkpoint{}, err
	}
	if err := os.WriteFile(filepath.Join(dest, "manifest.json"), data, 0o600); err != nil {
		return domain.Checkpoint{}, err
	}

	return domain.Checkpoint{
		ID:         id,
		SessionID:  sessionID,
		Workspace:  workspace,
		CreatedAt:  time.Now(),
		FileCount:  len(manifest.Files),
		TotalBytes: total,
	}, nil
}

// Restore revierte el workspace al estado de un checkpoint.
func (s *CheckpointStore) Restore(ctx context.Context, id string) error {
	dir, err := s.findByID(id)
	if err != nil {
		return err
	}
	manifest, err := s.loadManifest(dir)
	if err != nil {
		return err
	}
	if manifest.Workspace == "" {
		return os.ErrNotExist
	}
	for _, rel := range manifest.Files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		src := filepath.Join(dir, rel)
		// Evitar path traversal: el destino debe quedarse dentro del workspace.
		dst := filepath.Join(manifest.Workspace, rel)
		clean := filepath.Clean(dst)
		base := filepath.Clean(manifest.Workspace)
		if clean != base && !strings.HasPrefix(clean, base+string(os.PathSeparator)) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(clean), 0o755); err != nil {
			return err
		}
		data, rerr := os.ReadFile(src)
		if rerr != nil {
			continue
		}
		// #nosec G703 -- el destino se valida arriba para quedar dentro del
		// workspace (clean == base o prefijo de base); el manifest es local.
		if err := os.WriteFile(clean, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// List devuelve los checkpoints de una sesión, del más reciente al más viejo.
func (s *CheckpointStore) List(ctx context.Context, sessionID string, limit int) ([]domain.Checkpoint, error) {
	dir := filepath.Join(s.root, sessionID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	out := make([]domain.Checkpoint, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, lerr := s.loadManifest(filepath.Join(dir, e.Name()))
		if lerr != nil {
			continue
		}
		out = append(out, domain.Checkpoint{
			ID:        m.ID,
			SessionID: m.SessionID,
			Workspace: m.Workspace,
			CreatedAt: parseRFC3339(m.CreatedAt),
			FileCount: len(m.Files),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Prune elimina los checkpoints más antiguos de cada sesión dejando `keep`.
func (s *CheckpointStore) Prune(ctx context.Context, keep int) error {
	sessions, err := os.ReadDir(s.root)
	if err != nil {
		return nil
	}
	for _, se := range sessions {
		if !se.IsDir() {
			continue
		}
		dir := filepath.Join(s.root, se.Name())
		entries, _ := os.ReadDir(dir)
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Name() > entries[j].Name()
		})
		for i, e := range entries {
			if i >= keep {
				_ = os.RemoveAll(filepath.Join(dir, e.Name()))
			}
		}
	}
	return nil
}

// findByID localiza el directorio de un checkpoint por su ID.
func (s *CheckpointStore) findByID(id string) (string, error) {
	sessions, err := os.ReadDir(s.root)
	if err != nil {
		return "", domain.ErrNotFound
	}
	for _, se := range sessions {
		if !se.IsDir() {
			continue
		}
		candidate := filepath.Join(s.root, se.Name(), id)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", domain.ErrNotFound
}

func (s *CheckpointStore) loadManifest(dir string) (checkpointManifest, error) {
	var m checkpointManifest
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(data, &m)
	return m, err
}

func parseRFC3339(value string) time.Time {
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return t
}

var _ interface {
	Create(context.Context, string, string) (domain.Checkpoint, error)
	Restore(context.Context, string) error
	List(context.Context, string, int) ([]domain.Checkpoint, error)
	Prune(context.Context, int) error
} = (*CheckpointStore)(nil)
