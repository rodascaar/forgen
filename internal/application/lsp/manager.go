// Package lsp integra Language Server Protocol: detección de servidor por
// lenguaje y registro de herramientas de inteligencia de código.
package lsp

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"

	lspadapter "github.com/rodascaar/forgen/internal/adapters/out/lsp"
	"github.com/rodascaar/forgen/internal/application/tools"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
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

// DiagnosticsFor devuelve diagnósticos para un archivo (PostToolUse hook).
func (m *Manager) DiagnosticsFor(ctx context.Context, path string) string {
	if m == nil || m.client == nil {
		return ""
	}
	diags, err := m.client.Diagnostics(ctx, path)
	if err != nil || len(diags) == 0 {
		return ""
	}
	var out string
	for _, d := range diags {
		out += fmt.Sprintf("%s:%d:%d %s: %s\n", d.File, d.Line, d.Column, severityLabel(d.Severity), d.Message)
	}
	return out
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
	registry.Register(m.implementationTool())
	registry.Register(m.typeDefinitionTool())
	registry.Register(m.documentSymbolsTool())
	registry.Register(m.workspaceSymbolsTool())
	registry.Register(m.codeActionTool())
	registry.Register(m.completionTool())
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

func (m *Manager) implementationTool() tools.ToolDef {
	return tools.ToolDef{
		Tool: domain.Tool{Name: "lsp_implementation", Description: "Navega a la implementación del símbolo.", Status: domain.ToolStatusEnabled, Schema: objectSchema(map[string]any{"path": stringProp("Ruta"), "line": intProp("Línea"), "column": intProp("Columna")}, "path", "line", "column")},
		Execute: func(ctx context.Context, raw map[string]any) domain.ToolResult {
			path, _ := raw["path"].(string)
			line, col, err := textPosition(raw)
			if err != nil || path == "" {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("lsp_implementation requiere path/line/column")}
			}
			locs, err := m.client.Implementation(ctx, path, line, col)
			if err != nil {
				return domain.ToolResult{OK: false, Error: err}
			}
			if len(locs) == 0 {
				return domain.ToolResult{OK: true, Output: "(sin resultados)"}
			}
			out := ""
			for _, l := range locs {
				out += fmt.Sprintf("%s:%d:%d\n", l.File, l.Line, l.Column)
			}
			return domain.ToolResult{OK: true, Output: out}
		},
	}
}

func (m *Manager) typeDefinitionTool() tools.ToolDef {
	return tools.ToolDef{
		Tool: domain.Tool{Name: "lsp_type_definition", Description: "Navega a la definición del tipo del símbolo.", Status: domain.ToolStatusEnabled, Schema: objectSchema(map[string]any{"path": stringProp("Ruta"), "line": intProp("Línea"), "column": intProp("Columna")}, "path", "line", "column")},
		Execute: func(ctx context.Context, raw map[string]any) domain.ToolResult {
			path, _ := raw["path"].(string)
			line, col, err := textPosition(raw)
			if err != nil || path == "" {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("lsp_type_definition requiere path/line/column")}
			}
			locs, err := m.client.TypeDefinition(ctx, path, line, col)
			if err != nil {
				return domain.ToolResult{OK: false, Error: err}
			}
			if len(locs) == 0 {
				return domain.ToolResult{OK: true, Output: "(sin resultados)"}
			}
			out := ""
			for _, l := range locs {
				out += fmt.Sprintf("%s:%d:%d\n", l.File, l.Line, l.Column)
			}
			return domain.ToolResult{OK: true, Output: out}
		},
	}
}

func (m *Manager) documentSymbolsTool() tools.ToolDef {
	return tools.ToolDef{
		Tool: domain.Tool{
			Name: "lsp_document_symbols", Description: "Lista símbolos del documento (funciones, clases, variables).",
			Status: domain.ToolStatusEnabled, Schema: objectSchema(map[string]any{"path": stringProp("Ruta del archivo")}, "path"),
		},
		Execute: func(ctx context.Context, raw map[string]any) domain.ToolResult {
			path, _ := raw["path"].(string)
			if path == "" {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("lsp_document_symbols requiere 'path'")}
			}
			out, err := m.client.DocumentSymbols(ctx, path)
			if err != nil {
				return domain.ToolResult{OK: false, Error: err}
			}
			if out == "" || out == "null" {
				return domain.ToolResult{OK: true, Output: "(sin símbolos)"}
			}
			return domain.ToolResult{OK: true, Output: out}
		},
	}
}

func (m *Manager) workspaceSymbolsTool() tools.ToolDef {
	return tools.ToolDef{
		Tool: domain.Tool{
			Name: "lsp_workspace_symbols", Description: "Busca símbolos en todo el workspace por query.",
			Status: domain.ToolStatusEnabled, Schema: objectSchema(map[string]any{"query": stringProp("Query de búsqueda de símbolos")}, "query"),
		},
		Execute: func(ctx context.Context, raw map[string]any) domain.ToolResult {
			query, _ := raw["query"].(string)
			if query == "" {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("lsp_workspace_symbols requiere 'query'")}
			}
			out, err := m.client.WorkspaceSymbols(ctx, query)
			if err != nil {
				return domain.ToolResult{OK: false, Error: err}
			}
			if out == "" || out == "null" {
				return domain.ToolResult{OK: true, Output: "(sin resultados)"}
			}
			return domain.ToolResult{OK: true, Output: out}
		},
	}
}

func (m *Manager) codeActionTool() tools.ToolDef {
	return tools.ToolDef{
		Tool: domain.Tool{
			Name: "lsp_code_action", Description: "Obtiene quick fixes / code actions para un rango.",
			Status: domain.ToolStatusEnabled, Schema: objectSchema(map[string]any{
				"path": stringProp("Ruta del archivo"), "line": intProp("Línea inicio (1-based)"), "column": intProp("Columna inicio"),
				"end_line": intProp("Línea fin (1-based)"), "end_column": intProp("Columna fin"),
			}, "path", "line", "column"),
		},
		Execute: func(ctx context.Context, raw map[string]any) domain.ToolResult {
			path, _ := raw["path"].(string)
			line, col, err := textPosition(raw)
			if err != nil || path == "" {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("lsp_code_action requiere path/line/column")}
			}
			endLineF, _ := raw["end_line"].(float64)
			endColF, _ := raw["end_column"].(float64)
			endLine, endCol := int(endLineF), int(endColF)
			if endLine == 0 {
				endLine = line
			}
			if endCol == 0 {
				endCol = col
			}
			out, err := m.client.CodeAction(ctx, path, line, col, endLine, endCol)
			if err != nil {
				return domain.ToolResult{OK: false, Error: err}
			}
			if out == "" || out == "null" || out == "[]" {
				return domain.ToolResult{OK: true, Output: "(sin acciones)"}
			}
			return domain.ToolResult{OK: true, Output: out}
		},
	}
}

func (m *Manager) completionTool() tools.ToolDef {
	return tools.ToolDef{
		Tool: domain.Tool{
			Name: "lsp_completion", Description: "Obtiene completions en una posición.",
			Status: domain.ToolStatusEnabled, Schema: objectSchema(map[string]any{"path": stringProp("Ruta del archivo"), "line": intProp("Línea (1-based)"), "column": intProp("Columna (1-based)")}, "path", "line", "column"),
		},
		Execute: func(ctx context.Context, raw map[string]any) domain.ToolResult {
			path, _ := raw["path"].(string)
			line, col, err := textPosition(raw)
			if err != nil || path == "" {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("lsp_completion requiere path/line/column")}
			}
			out, err := m.client.Completion(ctx, path, line, col)
			if err != nil {
				return domain.ToolResult{OK: false, Error: err}
			}
			if out == "" || out == "null" {
				return domain.ToolResult{OK: true, Output: "(sin completions)"}
			}
			return domain.ToolResult{OK: true, Output: out}
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
