// Package lsp integra Language Server Protocol: detección de servidor por
// lenguaje y registro de herramientas de inteligencia de código.
package lsp

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"

	lspadapter "github.com/forgen/forgen/internal/adapters/out/lsp"
	"github.com/forgen/forgen/internal/application/tools"
	"github.com/forgen/forgen/internal/core/domain"
	"github.com/forgen/forgen/internal/core/ports"
)

// languageServer asocia un lenguaje a su servidor y languageId.
type languageServer struct {
	command    string
	args       []string
	languageID string
}

// registry asocia lenguajes detectados a sus servidores LSP.
var registry = map[string]languageServer{
	"Go":         {command: "gopls", languageID: "go"},
	"TypeScript": {command: "typescript-language-server", args: []string{"--stdio"}, languageID: "typescript"},
	"JavaScript": {command: "typescript-language-server", args: []string{"--stdio"}, languageID: "javascript"},
	"Rust":       {command: "rust-analyzer", languageID: "rust"},
	"Python":     {command: "pyright-langserver", args: []string{"--stdio"}, languageID: "python"},
}

// Manager arranca el language server del lenguaje dominante y registra sus tools.
type Manager struct {
	client *lspadapter.Client
	logger *slog.Logger
}

// NewManager detecta el lenguaje del workspace y arranca el server si existe.
// Devuelve (nil, nil) si no hay lenguaje o el binario del server no está instalado.
func NewManager(ctx context.Context, detector ports.LanguageDetector, fs ports.FileSystem, workspace string, logger *slog.Logger) *Manager {
	language, err := detector.Detect(ctx, workspace)
	if err != nil || language == "" {
		return nil
	}
	server, ok := registry[language]
	if !ok {
		return nil
	}
	if _, err := exec.LookPath(server.command); err != nil {
		logger.Debug("lsp: servidor no instalado", "command", server.command)
		return nil
	}

	client, err := lspadapter.NewClient(server.command, server.args, fs, workspace, server.languageID)
	if err != nil {
		logger.Warn("lsp: no se pudo arrancar", "command", server.command, "err", err)
		return nil
	}
	logger.Info("lsp.started", "language", language, "command", server.command)
	return &Manager{client: client, logger: logger}
}

// Close cierra el language server.
func (m *Manager) Close() {
	if m != nil && m.client != nil {
		_ = m.client.Close()
	}
}

// Syncer devuelve el sincronizador de documentos para el wrapper de FileSystem.
func (m *Manager) Syncer() lspadapter.DocumentSyncer {
	if m == nil {
		return nil
	}
	return m.client
}

// RegisterTools registra las herramientas LSP en el registry del agente.
func (m *Manager) RegisterTools(registry *tools.Registry) {
	if m == nil || m.client == nil {
		return
	}
	registry.Register(m.diagnosticsTool())
	registry.Register(m.hoverTool())
	registry.Register(m.definitionTool())
	registry.Register(m.referencesTool())
	registry.Register(m.renameTool())
}

// textPosition extrae (line, column) 1-based de los argumentos.
func textPosition(raw map[string]any) (line, column int, err error) {
	lineF, okLine := raw["line"].(float64)
	columnF, okColumn := raw["column"].(float64)
	if !okLine || !okColumn {
		return 0, 0, fmt.Errorf("se requieren 'line' y 'column'")
	}
	return int(lineF), int(columnF), nil
}

func (m *Manager) diagnosticsTool() tools.ToolDef {
	return tools.ToolDef{
		Tool: domain.Tool{
			Name:        "lsp_diagnostics",
			Description: "Obtiene errores, warnings y diagnósticos del lenguaje para un archivo.",
			Status:      domain.ToolStatusEnabled,
			Schema:      objectSchema(map[string]any{"path": stringProp("Ruta del archivo")}, "path"),
		},
		Execute: func(ctx context.Context, raw map[string]any) domain.ToolResult {
			path, _ := raw["path"].(string)
			if path == "" {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("lsp_diagnostics requiere 'path'")}
			}
			diagnostics, err := m.client.Diagnostics(ctx, path)
			if err != nil {
				return domain.ToolResult{OK: false, Error: err}
			}
			if len(diagnostics) == 0 {
				return domain.ToolResult{OK: true, Output: "(sin diagnósticos)"}
			}
			output := ""
			for _, diag := range diagnostics {
				output += fmt.Sprintf("%s:%d:%d %s: %s\n", diag.File, diag.Line, diag.Column, severityLabel(diag.Severity), diag.Message)
			}
			return domain.ToolResult{OK: true, Output: output}
		},
	}
}

func (m *Manager) hoverTool() tools.ToolDef {
	return tools.ToolDef{
		Tool: domain.Tool{
			Name:        "lsp_hover",
			Description: "Obtiene documentación y tipo del símbolo en una posición.",
			Status:      domain.ToolStatusEnabled,
			Schema:      objectSchema(map[string]any{"path": stringProp("Ruta del archivo"), "line": intProp("Línea (1-based)"), "column": intProp("Columna (1-based)")}, "path", "line", "column"),
		},
		Execute: func(ctx context.Context, raw map[string]any) domain.ToolResult {
			path, _ := raw["path"].(string)
			line, column, err := textPosition(raw)
			if err != nil || path == "" {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("lsp_hover requiere path/line/column")}
			}
			text, err := m.client.Hover(ctx, path, line, column)
			if err != nil {
				return domain.ToolResult{OK: false, Error: err}
			}
			if text == "" {
				return domain.ToolResult{OK: true, Output: "(sin documentación)"}
			}
			return domain.ToolResult{OK: true, Output: text}
		},
	}
}

func (m *Manager) definitionTool() tools.ToolDef {
	return m.locationTool("lsp_definition", "Navega a la definición del símbolo en una posición.", false)
}

func (m *Manager) referencesTool() tools.ToolDef {
	return m.locationTool("lsp_references", "Encuentra todas las referencias al símbolo en una posición.", true)
}

func (m *Manager) locationTool(name, description string, references bool) tools.ToolDef {
	return tools.ToolDef{
		Tool: domain.Tool{
			Name:        name,
			Description: description,
			Status:      domain.ToolStatusEnabled,
			Schema:      objectSchema(map[string]any{"path": stringProp("Ruta del archivo"), "line": intProp("Línea (1-based)"), "column": intProp("Columna (1-based)")}, "path", "line", "column"),
		},
		Execute: func(ctx context.Context, raw map[string]any) domain.ToolResult {
			path, _ := raw["path"].(string)
			line, column, err := textPosition(raw)
			if err != nil || path == "" {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("%s requiere path/line/column", name)}
			}
			var locations []ports.LSPLocation
			if references {
				locations, err = m.client.References(ctx, path, line, column)
			} else {
				locations, err = m.client.Definition(ctx, path, line, column)
			}
			if err != nil {
				return domain.ToolResult{OK: false, Error: err}
			}
			if len(locations) == 0 {
				return domain.ToolResult{OK: true, Output: "(sin resultados)"}
			}
			output := ""
			for _, location := range locations {
				output += fmt.Sprintf("%s:%d:%d\n", location.File, location.Line, location.Column)
			}
			return domain.ToolResult{OK: true, Output: output}
		},
	}
}

func (m *Manager) renameTool() tools.ToolDef {
	return tools.ToolDef{
		Tool: domain.Tool{
			Name:        "lsp_rename",
			Description: "Renombra un símbolo de forma segura en todos los archivos.",
			Status:      domain.ToolStatusEnabled,
			Schema: objectSchema(map[string]any{
				"path": stringProp("Ruta del archivo"), "line": intProp("Línea (1-based)"),
				"column": intProp("Columna (1-based)"), "new_name": stringProp("Nuevo nombre del símbolo"),
			}, "path", "line", "column", "new_name"),
		},
		Execute: func(ctx context.Context, raw map[string]any) domain.ToolResult {
			path, _ := raw["path"].(string)
			line, column, err := textPosition(raw)
			if err != nil || path == "" {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("lsp_rename requiere path/line/column")}
			}
			newName, _ := raw["new_name"].(string)
			if newName == "" {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("lsp_rename requiere 'new_name'")}
			}
			if err := m.client.Rename(ctx, path, line, column, newName); err != nil {
				return domain.ToolResult{OK: false, Error: err}
			}
			return domain.ToolResult{OK: true, Output: fmt.Sprintf("Símbolo renombrado a %s", newName)}
		},
	}
}

func severityLabel(severity ports.LSPDiagnosticSeverity) string {
	switch severity {
	case ports.LSPError:
		return "error"
	case ports.LSPWarning:
		return "warning"
	case ports.LSPInfo:
		return "info"
	case ports.LSPHint:
		return "hint"
	default:
		return "diag"
	}
}

// objectSchema y helpers de schema compartidos con las tools LSP.
func objectSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringProp(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func intProp(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}
