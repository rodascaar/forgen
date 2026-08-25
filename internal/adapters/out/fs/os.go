// Package fs implementa ports.FileSystem sobre el sistema real.
package fs

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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

// Read implementa ports.FileSystem.
func (o *OSFileSystem) Read(_ context.Context, path string) ([]byte, error) {
	data, err := os.ReadFile(o.resolve(path))
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Write implementa ports.FileSystem.
func (o *OSFileSystem) Write(_ context.Context, path string, data []byte) error {
	resolved := o.resolve(path)
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
	// Dividir el patrón en base + patrón relativo para listar correctamente.
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
func (o *OSFileSystem) Search(_ context.Context, root, query, include string) ([]ports.SearchMatch, error) {
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
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for index, line := range strings.Split(string(data), "\n") {
			if len(matches) >= maxSearchMatches {
				break
			}
			if regex.MatchString(line) {
				matches = append(matches, ports.SearchMatch{
					File: filepath.Clean(relativePath(base, path)),
					Line: index + 1,
					Text: strings.TrimSpace(line),
				})
			}
		}
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
	for _, line := range strings.Split(string(data), "\n") {
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
