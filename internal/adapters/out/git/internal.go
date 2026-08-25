// Package git implementa ports.Git combinando el git real del usuario con el
// versionado interno de forgen. El versionado interno es un tracking propio por
// snapshots del workspace que funciona SIEMPRE, aunque el workspace no sea un
// repositorio git del usuario.
package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rodascaar/forgen/internal/core/ports"
)

// internalStore persiste snapshots del workspace por directorio para ofrecer un
// status/diff interno que nunca depende del git real del usuario.
type internalStore struct {
	root string
}

// internalManifest describe el contenido de un snapshot interno.
type internalManifest struct {
	Workspace string              `json:"workspace"`
	Files     map[string]fileInfo `json:"files"`
}

// fileInfo captura el estado de un archivo en un snapshot.
type fileInfo struct {
	Size  int64  `json:"size"`
	MTime string `json:"mtime"`
}

// ignoredInternalDir devuelve true para directorios que no se trackean.
func ignoredInternalDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".venv", "venv", "dist", "bin",
		".cache", ".idea", ".vscode", ".opencode", ".claude", "checkpoints":
		return true
	}
	return false
}

// NewInternalStore construye el store en un directorio raíz.
func NewInternalStore(root string) *internalStore {
	return &internalStore{root: root}
}

// dirFor deriva el directorio del snapshot de un workspace (hash estable).
func (s *internalStore) dirFor(workdir string) string {
	abs, err := filepath.Abs(workdir)
	if err != nil {
		abs = workdir
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(s.root, hex.EncodeToString(sum[:])[:16])
}

// manifestPath devuelve la ruta del manifest del snapshot de un workspace.
func (s *internalStore) manifestPath(workdir string) string {
	return filepath.Join(s.dirFor(workdir), "manifest.json")
}

// snapshot toma un snapshot del estado actual del workspace. Es idempotente y
// nunca falla ante directorios inexistentes.
func (s *internalStore) snapshot(ctx context.Context, workdir string) (internalManifest, error) {
	manifest := internalManifest{Workspace: workdir, Files: map[string]fileInfo{}}
	_ = filepath.WalkDir(workdir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			if path != workdir && ignoredInternalDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(workdir, path)
		if rerr != nil || strings.HasPrefix(rel, "..") {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		manifest.Files[filepath.ToSlash(rel)] = fileInfo{
			Size:  info.Size(),
			MTime: info.ModTime().UTC().Format(time.RFC3339Nano),
		}
		return nil
	})
	return manifest, nil
}

// Baseline carga (o crea si no existe) el snapshot de línea-base de un workspace.
func (s *internalStore) Baseline(ctx context.Context, workdir string) (internalManifest, error) {
	if data, err := os.ReadFile(s.manifestPath(workdir)); err == nil {
		var m internalManifest
		if json.Unmarshal(data, &m) == nil {
			return m, nil
		}
	}
	base, err := s.snapshot(ctx, workdir)
	if err != nil {
		return internalManifest{}, err
	}
	s.save(base, workdir)
	return base, nil
}

// ResetBaseline vuelve a tomar el snapshot actual como nueva línea-base.
func (s *internalStore) ResetBaseline(ctx context.Context, workdir string) (internalManifest, error) {
	base, err := s.snapshot(ctx, workdir)
	if err != nil {
		return internalManifest{}, err
	}
	s.save(base, workdir)
	return base, nil
}

// save persiste el manifest en disco.
func (s *internalStore) save(m internalManifest, workdir string) {
	dir := s.dirFor(workdir)
	_ = os.MkdirAll(dir, 0o755)
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o600)
}

// changedFiles compara el estado actual con la línea-base y devuelve una lista
// porcelain interna (M/A/D) de los archivos que difieren.
func (s *internalStore) changedFiles(ctx context.Context, workdir string) ([]string, error) {
	base, err := s.Baseline(ctx, workdir)
	if err != nil {
		return nil, err
	}
	current, err := s.snapshot(ctx, workdir)
	if err != nil {
		return nil, err
	}
	changed := make([]string, 0, len(current.Files)+len(base.Files))
	seen := map[string]bool{}
	for rel, cur := range current.Files {
		seen[rel] = true
		prev, ok := base.Files[rel]
		if !ok {
			changed = append(changed, "A "+rel)
			continue
		}
		if prev.Size != cur.Size || prev.MTime != cur.MTime {
			changed = append(changed, "M "+rel)
		}
	}
	for rel := range base.Files {
		if !seen[rel] {
			changed = append(changed, "D "+rel)
		}
	}
	sort.Strings(changed)
	return changed, nil
}

// Status produce un estado porcelain interno del workspace.
func (s *internalStore) Status(ctx context.Context, workdir string) (string, error) {
	changed, err := s.changedFiles(ctx, workdir)
	if err != nil {
		return "", err
	}
	return strings.Join(changed, "\n"), nil
}

// Diff devuelve una descripción legible de los cambios respecto a la línea-base
// (por archivo, con contadores). Es un diff "interno": no depende del git real.
func (s *internalStore) Diff(ctx context.Context, workdir string) (string, error) {
	changed, err := s.changedFiles(ctx, workdir)
	if err != nil {
		return "", err
	}
	if len(changed) == 0 {
		return "", nil
	}
	var builder strings.Builder
	builder.WriteString("Cambios internos (forgen):\n")
	for _, line := range changed {
		builder.WriteString("  " + line + "\n")
	}
	builder.WriteString("\nLínea-base: " + s.manifestPath(workdir) + "\n")
	return strings.TrimRight(builder.String(), "\n"), nil
}

// combinedGit une el git real del usuario con el tracking interno de forgen.
// Si el workspace es un repo git real, delega en él; si no, usa el interno para
// que las tools nunca fallen en rojo.
type combinedGit struct {
	real    ports.Git
	internal *internalStore
}

// NewCombined construye el adapter de git combinado.
func NewCombined(real ports.Git, internalRoot string) ports.Git {
	return &combinedGit{real: real, internal: NewInternalStore(internalRoot)}
}

// IsRepo reporta true si el workspace es un repo git real.
func (c *combinedGit) IsRepo(ctx context.Context, workdir string) (bool, error) {
	return c.real.IsRepo(ctx, workdir)
}

// Status usa el git real si hay repo; si no, el tracking interno.
func (c *combinedGit) Status(ctx context.Context, workdir string) (string, error) {
	if isRepo, _ := c.real.IsRepo(ctx, workdir); isRepo {
		return c.real.Status(ctx, workdir)
	}
	return c.internal.Status(ctx, workdir)
}

// Diff usa el git real si hay repo; si no, el tracking interno.
func (c *combinedGit) Diff(ctx context.Context, workdir string, staged bool) (string, error) {
	if isRepo, _ := c.real.IsRepo(ctx, workdir); isRepo {
		return c.real.Diff(ctx, workdir, staged)
	}
	return c.internal.Diff(ctx, workdir)
}

var _ ports.Git = (*combinedGit)(nil)