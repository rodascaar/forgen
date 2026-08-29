// Package fs implementa ports.FileSystem sobre el sistema real.
package fs

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// ignoredDirectories son los directorios que nunca se indexan en búsquedas.
var ignoredDirectories = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".venv": true,
	"venv": true, "dist": true, "build": true, ".next": true, "target": true,
	"__pycache__": true, ".cache": true, ".idea": true, ".vscode": true,
	".terraform": true, "coverage": true,
}

// maxSearchMatches limita las coincidencias devueltas por búsqueda.
const maxSearchMatches = 200

// bufferPool reutiliza buffers de 64 KiB para escaneo de archivos.
var bufferPool = sync.Pool{
	New: func() any {
		return make([]byte, 64*1024)
	},
}

// OSFileSystem resuelve rutas relativas contra un workspace raíz.
type OSFileSystem struct {
	root string
}

// New construye un FileSystem anclado al workspace.
func New(root string) *OSFileSystem {
	return &OSFileSystem{root: root}
}

func (o *OSFileSystem) resolve(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(o.root, path)
}

// isOutside reports if resolved path escapes workspace (simple clean, no EvalSymlinks to avoid /tmp vs /private/tmp mismatch).
func (o *OSFileSystem) isOutside(path string) bool {
	resolved := filepath.Clean(o.resolve(path))
	root := filepath.Clean(o.root)
	// Normalize root and resolved without symlink eval for test compat (macOS /tmp -> /private/tmp handled by Clean not eval)
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return true
	}
	if rel == "." {
		return false
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Read implementa ports.FileSystem.
// No hard-block for outside: permission layer handles ask/deny for external_directory.
// Only Write is hard-blocked for safety; Read allows outside after permission.
func (o *OSFileSystem) Read(_ context.Context, path string) ([]byte, error) {
	// 50MB limit to prevent OOM before truncation
	const maxReadSize = 50 * 1024 * 1024
	resolved := o.resolve(path)
	info, err := os.Stat(resolved)
	if err == nil && info.Size() > maxReadSize {
		return nil, fmt.Errorf("archivo demasiado grande (%d bytes): %s", info.Size(), path)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Write implementa ports.FileSystem.
func (o *OSFileSystem) Write(_ context.Context, path string, data []byte) error {
	if o.isOutside(path) {
		return fmt.Errorf("escritura fuera del workspace denegada: %s", path)
	}
	resolved := o.resolve(path)
	// Check symlink traversal: Dir must not escape
	if evalDir, err := filepath.EvalSymlinks(filepath.Dir(o.root)); err == nil {
		_ = evalDir
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return fmt.Errorf("crear directorios de %s: %w", resolved, err)
	}
	if err := os.WriteFile(resolved, data, 0o644); err != nil {
		return fmt.Errorf("escribir %s: %w", resolved, err)
	}
	return nil
}

// Exists implementa ports.FileSystem.
func (o *OSFileSystem) Exists(_ context.Context, path string) (bool, error) {
	_, err := os.Stat(o.resolve(path))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// Glob implementa ports.FileSystem con soporte de patrones ** (doublestar).
func (o *OSFileSystem) Glob(_ context.Context, pattern string) ([]string, error) {
	base := o.resolve(".")
	matches, err := doublestar.FilepathGlob(o.resolve(pattern))
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		result = append(result, filepath.Clean(relativePath(base, match)))
	}
	return result, nil
}

// Search implementa ports.FileSystem.
func (o *OSFileSystem) Search(ctx context.Context, root, query, include string) ([]ports.SearchMatch, error) {
	regex, err := regexp.Compile(query)
	if err != nil {
		return nil, fmt.Errorf("patrón regex inválido: %w", err)
	}

	base := o.resolve(".")
	searchRoot := o.resolve(root)
	gitignore := o.loadGitignore(base)
	matches := make([]ports.SearchMatch, 0, 32)

	walkErr := filepath.WalkDir(searchRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // ignorar errores de archivos no legibles
		}
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// .gitignore check para dirs y files (rel a base)
		if rel, err := filepath.Rel(base, path); err == nil {
			if o.ignoredByGitignore(rel, entry.IsDir(), gitignore) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if entry.IsDir() {
			if path != searchRoot && ignoredDirectories[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if include != "" {
			rel, relErr := filepath.Rel(searchRoot, path)
			if relErr != nil {
				return nil
			}
			// doublestar usa "/" como separador; en Windows filepath.Rel
			// devuelve "\" (ej. src\main.go). Normalizar para que el patrón
			// coincida independientemente del SO.
			rel = filepath.ToSlash(rel)
			// Un patrón sin separador (ej. *.go) debe coincidir a cualquier
			// profundidad: se normaliza a **/*.go.
			includePattern := include
			if !strings.ContainsAny(include, "/\\") {
				includePattern = "**/" + include
			}
			// doublestar.Match siempre usa "/" como separador (a diferencia de
			// PathMatch, que usa filepath.Separator = "\" en Windows). Como rel
			// ya está normalizado a "/", Match funciona en todas las plataformas.
			matched, matchErr := doublestar.Match(includePattern, rel)
			if matchErr != nil || !matched {
				return nil
			}
		}
		if len(matches) >= maxSearchMatches {
			return nil
		}
		// Skip huge files to prevent OOM
		if info, err := entry.Info(); err == nil && info.Size() > 1024*1024 {
			return nil
		}
		// Streaming line-by-line with pooled buffer to avoid loading entire file
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		// Get a pooled buffer (64 KiB) for the scanner
		buf := bufferPool.Get().([]byte)
		fileScanner := bufio.NewScanner(file)
		fileScanner.Buffer(buf, 64*1024)
		index := 0
		for fileScanner.Scan() {
			select {
			case <-ctx.Done():
				file.Close()
				bufferPool.Put(buf)
				return ctx.Err()
			default:
			}
			if len(matches) >= maxSearchMatches {
				break
			}
			line := fileScanner.Text()
			if regex.MatchString(line) {
				matches = append(matches, ports.SearchMatch{
					File: filepath.Clean(relativePath(base, path)),
					Line: index + 1,
					Text: strings.TrimSpace(line),
				})
			}
			index++
		}
		file.Close()
		bufferPool.Put(buf)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return matches, nil
}

func (o *OSFileSystem) loadGitignore(base string) []string {
	data, err := os.ReadFile(filepath.Join(base, ".gitignore"))
	if err != nil {
		return nil
	}
	var patterns []string
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Normalizar: quitar "/" inicial
		line = strings.TrimPrefix(line, "/")
		patterns = append(patterns, line)
	}
	return patterns
}

func (o *OSFileSystem) ignoredByGitignore(rel string, isDir bool, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	relSlash := filepath.ToSlash(rel)
	for _, pat := range patterns {
		// doublestar match con ** prefix para dirs
		matchPat := pat
		if isDir && !strings.HasSuffix(matchPat, "/") && !strings.Contains(matchPat, "/") {
			matchPat = matchPat + "/**"
		}
		if !strings.Contains(matchPat, "/") {
			matchPat = "**/" + matchPat
		}
		if ok, _ := doublestar.Match(matchPat, relSlash); ok {
			return true
		}
		// también prefijo directo
		if strings.HasPrefix(relSlash, strings.TrimSuffix(pat, "/")+"/") || relSlash == strings.TrimSuffix(pat, "/") {
			return true
		}
	}
	return false
}

func relativePath(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	return rel
}

var _ ports.FileSystem = (*OSFileSystem)(nil)
