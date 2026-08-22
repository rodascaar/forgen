package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// ToolFunc ejecuta la herramienta con argumentos tipados.
type ToolFunc[ArgType any] func(ctx context.Context, args ArgType) domain.ToolResult

// ToolDef une el esquema expuesto al modelo con la implementación.
type ToolDef struct {
	domain.Tool
	Execute func(ctx context.Context, raw map[string]any) domain.ToolResult
}

// maxOutputChars es el límite por defecto de salida enviada al modelo.
const maxOutputChars = 30000

// Registry implementa ports.ToolExecutor registrando herramientas.
type Registry struct {
	tools       []ToolDef
	byName      map[string]ToolDef
	fs          ports.FileSystem
	executor    ports.Executor
	git         ports.Git
	outputLimit int
}

// NewRegistry construye el registro con las herramientas integradas.
func NewRegistry(fs ports.FileSystem, executor ports.Executor, git ports.Git, outputLimit int) *Registry {
	if outputLimit <= 0 {
		outputLimit = maxOutputChars
	}
	registry := &Registry{
		byName:      make(map[string]ToolDef),
		fs:          fs,
		executor:    executor,
		git:         git,
		outputLimit: outputLimit,
	}
	registry.register(registry.readTool())
	registry.register(registry.writeTool())
	registry.register(registry.editTool())
	registry.register(registry.globTool())
	registry.register(registry.grepTool())
	registry.register(registry.bashTool())
	registry.register(registry.gitStatusTool())
	registry.register(registry.gitDiffTool())
	return registry
}

func (r *Registry) register(tool ToolDef) {
	r.tools = append(r.tools, tool)
	r.byName[tool.Name] = tool
}

// Register añade una herramienta externa al registro (ej. skills, MCP).
func (r *Registry) Register(tool ToolDef) {
	r.register(tool)
}

// SetOutputLimit actualiza el límite de salida enviada al modelo.
func (r *Registry) SetOutputLimit(limit int) {
	if limit > 0 {
		r.outputLimit = limit
	}
}

// newGenericTool construye una herramienta tipada con esquema y ejecución.
func newGenericTool[ArgType any](name, description string, schema map[string]any, fn ToolFunc[ArgType]) ToolDef {
	return NewGenericTool(name, description, schema, fn)
}

// NewGenericTool is the exported version of newGenericTool for use by other packages.
func NewGenericTool[ArgType any](name, description string, schema map[string]any, fn ToolFunc[ArgType]) ToolDef {
	return ToolDef{
		Tool: domain.Tool{
			Name:        name,
			Description: description,
			Status:      domain.ToolStatusEnabled,
			Schema:      schema,
		},
		Execute: func(ctx context.Context, raw map[string]any) domain.ToolResult {
			args, err := decodeArgs[ArgType](raw)
			if err != nil {
				return domain.ToolResult{OK: false, Error: err}
			}
			return fn(ctx, args)
		},
	}
}

// ListTools implementa ports.ToolExecutor.
func (r *Registry) ListTools() []domain.Tool {
	tools := make([]domain.Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		if tool.Enabled() {
			tools = append(tools, tool.Tool)
		}
	}
	return tools
}

// LookupTools implementa ports.ToolExecutor.
func (r *Registry) LookupTools(names []string) []domain.Tool {
	if len(names) == 0 {
		return r.ListTools()
	}
	tools := make([]domain.Tool, 0, len(names))
	for _, name := range names {
		if tool, ok := r.byName[name]; ok && tool.Enabled() {
			tools = append(tools, tool.Tool)
		}
	}
	return tools
}

// Execute implementa ports.ToolExecutor.
func (r *Registry) Execute(ctx context.Context, call domain.ToolCall) domain.ToolResult {
	tool, ok := r.byName[call.Name]
	if !ok {
		return domain.ToolResult{
			ToolCallID: call.ID,
			OK:         false,
			Error:      fmt.Errorf("herramienta %q no registrada", call.Name),
		}
	}
	result := tool.Execute(ctx, call.Arguments)
	result.ToolCallID = call.ID
	result.Output = summarizeResult(result.Output, r.outputLimit)
	return result
}

// -- Definiciones de herramientas integradas --

type readArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

func (r *Registry) readTool() ToolDef {
	return newGenericTool("read", "Lee el contenido de un archivo (con offset y límite opcionales para archivos grandes).",
		objectSchema(map[string]map[string]any{
			"path":   stringProp("Ruta del archivo relativa o absoluta"),
			"offset": intProp("Línea inicial opcional (1-based)"),
			"limit":  intProp("Máximo de líneas a leer opcional"),
		}, "path"),
		func(ctx context.Context, args readArgs) domain.ToolResult {
			data, err := r.fs.Read(ctx, args.Path)
			if err != nil {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("leer %s: %w", args.Path, err)}
			}
			content := string(data)
			if args.Offset > 0 {
				lines := strings.Split(content, "\n")
				start := args.Offset - 1
				if start >= len(lines) {
					return domain.ToolResult{OK: true, Output: "(offset fuera de rango)"}
				}
				end := len(lines)
				if args.Limit > 0 && start+args.Limit < end {
					end = start + args.Limit
				}
				content = strings.Join(lines[start:end], "\n")
			} else if args.Limit > 0 {
				lines := strings.Split(content, "\n")
				if args.Limit < len(lines) {
					lines = lines[:args.Limit]
				}
				content = strings.Join(lines, "\n")
			}
			return domain.ToolResult{OK: true, Output: content}
		})
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (r *Registry) writeTool() ToolDef {
	return newGenericTool("write", "Escribe contenido en un archivo (crea directorios y sobreescribe).",
		objectSchema(map[string]map[string]any{
			"path":    stringProp("Ruta del archivo a escribir"),
			"content": stringProp("Contenido completo del archivo"),
		}, "path", "content"),
		func(ctx context.Context, args writeArgs) domain.ToolResult {
			if err := r.fs.Write(ctx, args.Path, []byte(args.Content)); err != nil {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("escribir %s: %w", args.Path, err)}
			}
			return domain.ToolResult{OK: true, Output: fmt.Sprintf("Archivo %s escrito (%d bytes)", args.Path, len(args.Content))}
		})
}

type editArgs struct {
	Path      string `json:"path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

func (r *Registry) editTool() ToolDef {
	return newGenericTool("edit", "Reemplaza la primera ocurrencia exacta de old_string por new_string en un archivo.",
		objectSchema(map[string]map[string]any{
			"path":       stringProp("Ruta del archivo a editar"),
			"old_string": stringProp("Texto exacto a reemplazar (debe aparecer una vez)"),
			"new_string": stringProp("Texto de reemplazo"),
		}, "path", "old_string", "new_string"),
		func(ctx context.Context, args editArgs) domain.ToolResult {
			data, err := r.fs.Read(ctx, args.Path)
			if err != nil {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("leer %s: %w", args.Path, err)}
			}
			content := string(data)
			count := strings.Count(content, args.OldString)
			if count == 0 {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("old_string no encontrado en %s", args.Path)}
			}
			if count > 1 {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("old_string aparece %d veces en %s; incluye más contexto", count, args.Path)}
			}
			content = strings.Replace(content, args.OldString, args.NewString, 1)
			if err := r.fs.Write(ctx, args.Path, []byte(content)); err != nil {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("escribir %s: %w", args.Path, err)}
			}
			return domain.ToolResult{OK: true, Output: fmt.Sprintf("Editado %s", args.Path)}
		})
}

type globArgs struct {
	Pattern string `json:"pattern"`
}

func (r *Registry) globTool() ToolDef {
	return newGenericTool("glob", "Encuentra archivos por patrón glob (ej: **/*.go).",
		objectSchema(map[string]map[string]any{
			"pattern": stringProp("Patrón glob"),
		}, "pattern"),
		func(ctx context.Context, args globArgs) domain.ToolResult {
			matches, err := r.fs.Glob(ctx, args.Pattern)
			if err != nil {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("glob %q: %w", args.Pattern, err)}
			}
			if len(matches) == 0 {
				return domain.ToolResult{OK: true, Output: "(sin coincidencias)"}
			}
			return domain.ToolResult{OK: true, Output: strings.Join(matches, "\n")}
		})
}

type grepArgs struct {
	Query   string `json:"query"`
	Path    string `json:"path,omitempty"`
	Include string `json:"include,omitempty"`
}

func (r *Registry) grepTool() ToolDef {
	return newGenericTool("grep", "Busca un patrón regex en archivos del proyecto y devuelve coincidencias con línea y texto.",
		objectSchema(map[string]map[string]any{
			"query":   stringProp("Patrón regex a buscar"),
			"path":    stringProp("Directorio raíz de búsqueda (por defecto el workspace)"),
			"include": stringProp("Filtro glob opcional, ej: *.go, *.{ts,tsx}"),
		}, "query"),
		func(ctx context.Context, args grepArgs) domain.ToolResult {
			root := args.Path
			if root == "" {
				root = "."
			}
			matches, err := r.fs.Search(ctx, root, args.Query, args.Include)
			if err != nil {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("buscar %q: %w", args.Query, err)}
			}
			if len(matches) == 0 {
				return domain.ToolResult{OK: true, Output: fmt.Sprintf("(sin coincidencias para %q)", args.Query)}
			}
			var builder strings.Builder
			for _, match := range matches {
				fmt.Fprintf(&builder, "%s:%d: %s\n", match.File, match.Line, match.Text)
			}
			return domain.ToolResult{OK: true, Output: builder.String()}
		})
}

type bashArgs struct {
	Command string `json:"command"`
	Workdir string `json:"workdir,omitempty"`
}

func (r *Registry) bashTool() ToolDef {
	return newGenericTool("bash", "Ejecuta un comando de shell en el workspace y devuelve stdout/stderr y exit code.",
		objectSchema(map[string]map[string]any{
			"command": stringProp("Comando shell a ejecutar"),
			"workdir": stringProp("Directorio de trabajo opcional"),
		}, "command"),
		func(ctx context.Context, args bashArgs) domain.ToolResult {
			workdir := args.Workdir
			if workdir == "" {
				workdir = "."
			}
			result, err := r.executor.Execute(ctx, args.Command, workdir, nil)
			if err != nil {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("ejecutar %q: %w", args.Command, err)}
			}
			output := result.Stdout
			if result.Stderr != "" {
				output += "\n[stderr]\n" + result.Stderr
			}
			if result.ExitCode != 0 {
				return domain.ToolResult{OK: true, Output: fmt.Sprintf("exit code %d\n%s", result.ExitCode, output)}
			}
			return domain.ToolResult{OK: true, Output: output}
		})
}

func (r *Registry) gitStatusTool() ToolDef {
	return newGenericTool("git_status", "Devuelve el estado del working tree de git (porcelain).",
		objectSchema(nil),
		func(ctx context.Context, _ map[string]any) domain.ToolResult {
			status, err := r.git.Status(ctx, ".")
			if err != nil {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("git status: %w", err)}
			}
			if status == "" {
				return domain.ToolResult{OK: true, Output: "(working tree limpio)"}
			}
			return domain.ToolResult{OK: true, Output: status}
		})
}

type gitDiffArgs struct {
	Staged bool `json:"staged,omitempty"`
}

func (r *Registry) gitDiffTool() ToolDef {
	return newGenericTool("git_diff", "Devuelve el diff del working tree (no stageado por defecto).",
		objectSchema(map[string]map[string]any{
			"staged": boolProp("Si true, devuelve el diff stageado"),
		}),
		func(ctx context.Context, args gitDiffArgs) domain.ToolResult {
			diff, err := r.git.Diff(ctx, ".", args.Staged)
			if err != nil {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("git diff: %w", err)}
			}
			if diff == "" {
				return domain.ToolResult{OK: true, Output: "(sin cambios)"}
			}
			return domain.ToolResult{OK: true, Output: diff}
		})
}

// FilePathFor resuelve una ruta relativa contra el workspace.
func FilePathFor(workspace, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workspace, path)
}

// EnsureWorkspace verifica que el directorio exista.
func EnsureWorkspace(workspace string) error {
	info, err := os.Stat(workspace)
	if err != nil {
		return fmt.Errorf("workspace %q: %w", workspace, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace %q no es un directorio", workspace)
	}
	return nil
}
