package ports

import (
	"context"
)

// FileSystem abstrae el acceso al sistema de archivos para que las tools
// sean testables sin tocar disco real.
type FileSystem interface {
	// Read devuelve el contenido del archivo.
	Read(ctx context.Context, path string) ([]byte, error)
	// Write persiste contenido en un archivo (crea directorios si faltan).
	Write(ctx context.Context, path string, data []byte) error
	// Exists indica si una ruta existe.
	Exists(ctx context.Context, path string) (bool, error)
	// Glob devuelve las rutas que coinciden con el patrón.
	Glob(ctx context.Context, pattern string) ([]string, error)
	// Search busca un patrón regex en los archivos de root (respetando
	// el include opcional) y devuelve las coincidencias con línea y texto.
	Search(ctx context.Context, root string, query string, include string) ([]SearchMatch, error)
}

// SearchMatch es una coincidencia de búsqueda en un archivo.
type SearchMatch struct {
	File string
	Line int
	Text string
}

// Executor ejecuta comandos del sistema de forma controlada.
type Executor interface {
	// Execute ejecuta un comando y devuelve stdout, stderr y exit code.
	Execute(ctx context.Context, command string, workdir string, env []string) (ExecResult, error)
}

// ExecResult es el resultado de un comando.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Git abstrae operaciones de git de solo lectura.
type Git interface {
	// Status devuelve el estado del working tree (porcelain).
	Status(ctx context.Context, workdir string) (string, error)
	// Diff devuelve el diff no stageado del working tree.
	Diff(ctx context.Context, workdir string, staged bool) (string, error)
	// IsRepo indica si el directorio es un repositorio git.
	IsRepo(ctx context.Context, workdir string) (bool, error)
}

// LanguageDetector identifica el lenguaje de programación de un proyecto.
type LanguageDetector interface {
	// Detect devuelve el lenguaje dominante en un directorio.
	Detect(ctx context.Context, dir string) (string, error)
}

// ToolchainProbe descubre el toolchain (manifests, gestores, comandos) de un proyecto.
type ToolchainProbe interface {
	// Probe devuelve un resumen legible del toolchain detectado.
	Probe(ctx context.Context, dir string) (string, error)
}
