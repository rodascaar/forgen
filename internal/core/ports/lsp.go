package ports

import "context"

// LSPDiagnosticSeverity es la gravedad de un diagnóstico.
type LSPDiagnosticSeverity int

const (
	LSPError   LSPDiagnosticSeverity = 1
	LSPWarning LSPDiagnosticSeverity = 2
	LSPInfo    LSPDiagnosticSeverity = 3
	LSPHint    LSPDiagnosticSeverity = 4
)

// LSPDiagnostic es un problema detectado por el language server.
type LSPDiagnostic struct {
	File     string
	Line     int // 1-based
	Column   int // 1-based
	Severity LSPDiagnosticSeverity
	Message  string
}

// LSPLocation es una posición dentro de un archivo.
type LSPLocation struct {
	File      string
	Line      int // 1-based
	Column    int // 1-based
	EndLine   int
	EndColumn int
}

// LSPClient es el puerto hacia un Language Server Protocol.
type LSPClient interface {
	// Diagnostics devuelve los diagnósticos del archivo (abre el documento).
	Diagnostics(ctx context.Context, path string) ([]LSPDiagnostic, error)
	// Hover devuelve la documentación del símbolo en (line, column) 1-based.
	Hover(ctx context.Context, path string, line, column int) (string, error)
	// Definition devuelve las ubicaciones de la definición del símbolo.
	Definition(ctx context.Context, path string, line, column int) ([]LSPLocation, error)
	// References devuelve todas las referencias al símbolo.
	References(ctx context.Context, path string, line, column int) ([]LSPLocation, error)
	// Rename renombra el símbolo y aplica los cambios al workspace.
	Rename(ctx context.Context, path string, line, column int, newName string) error
	// Close cierra la conexión con el language server.
	Close() error
}
