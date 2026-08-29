package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

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
	mu          sync.RWMutex
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
	registry.register(registry.readManyTool())
	registry.register(registry.writeTool())
	registry.register(registry.editTool())
	registry.register(registry.globTool())
	registry.register(registry.grepTool())
	registry.register(registry.bashTool())
	registry.register(registry.gitStatusTool())
	registry.register(registry.gitDiffTool())
	registry.register(registry.applyPatchTool())
	registry.register(registry.lsTool())
	return registry
}

func (r *Registry) register(tool ToolDef) {
	if tool.Name == "" {
		return
	}
	r.tools = append(r.tools, tool)
	r.byName[tool.Name] = tool
}

// Register añade una herramienta externa al registro (ej. skills, MCP).
func (r *Registry) Register(tool ToolDef) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.register(tool)
}

// SetOutputLimit actualiza el límite de salida enviada al modelo.
func (r *Registry) SetOutputLimit(limit int) {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.mu.RLock()
	defer r.mu.RUnlock()
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
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(names) == 0 {
		tools := make([]domain.Tool, 0, len(r.tools))
		for _, tool := range r.tools {
			if tool.Enabled() {
				tools = append(tools, tool.Tool)
			}
		}
		return tools
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
	r.mu.RLock()
	tool, ok := r.byName[call.Name]
	limit := r.outputLimit
	var hint string
	if !ok {
		names := make([]string, 0, len(r.tools))
		for _, t := range r.tools {
			names = append(names, t.Name)
		}
		hint = ". Tools disponibles: " + strings.Join(names, ", ")
	}
	r.mu.RUnlock()
	if !ok {
		// Error accionable: deja claro que NO es un fallo de forgen sino que el
		// modelo intentó una tool que no existe, y sugiere cómo resolverlo.
		return domain.ToolResult{
			ToolCallID: call.ID,
			OK:         false,
			Error:      fmt.Errorf("herramienta %q no registrada%s%s", call.Name, shellHint(call.Name), hint),
		}
	}
	result := tool.Execute(ctx, call.Arguments)
	result.ToolCallID = call.ID
	result.Output = summarizeResult(result.Output, limit)
	return result
}

// shellHint indica que, si el modelo intentó un comando shell directo, debe
// usar la herramienta bash en su lugar.
func shellHint(name string) string {
	switch name {
	case "ls", "pwd", "cat", "cd", "mkdir", "rm", "mv", "cp", "touch", "find", "grep", "curl", "echo", "which", "whoami", "head", "tail", "ps", "docker", "git":
		return " — usa la tool `bash` para ejecutar comandos shell"
	}
	return ""
}

// availableToolsHint lista las tools registradas para orientar al modelo.
func availableToolsHint(r *Registry) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for _, t := range r.tools {
		names = append(names, t.Name)
	}
	return ". Tools disponibles: " + strings.Join(names, ", ")
}

// -- Definiciones de herramientas integradas --

type readArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

func (r *Registry) readTool() ToolDef {
	return newGenericTool("read", "Lee un archivo (offset/limit para paginar). Para 2+ archivos usa read_many_files en una sola llamada.",
		objectSchema(map[string]any{
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

type readManyArgs struct {
	Paths []string `json:"paths"`
}

func (r *Registry) readManyTool() ToolDef {
	return newGenericTool("read_many_files", "Lee múltiples archivos (2-10) en una sola llamada. Ahorra turnos frente a varias read.",
		objectSchema(map[string]any{
			"paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Lista de rutas a leer (relativas o absolutas, 2-10 archivos)"},
		}, "paths"),
		func(ctx context.Context, args readManyArgs) domain.ToolResult {
			if len(args.Paths) == 0 {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("paths vacío")}
			}
			if len(args.Paths) > 10 {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("máximo 10 archivos por llamada, recibidos %d", len(args.Paths))}
			}
			var builder strings.Builder
			for i, p := range args.Paths {
				data, err := r.fs.Read(ctx, p)
				if err != nil {
					fmt.Fprintf(&builder, "=== %s ===\nERROR: %v\n", p, err)
				} else {
					fmt.Fprintf(&builder, "=== %s ===\n%s\n", p, string(data))
				}
				if i < len(args.Paths)-1 {
					builder.WriteString("\n")
				}
			}
			return domain.ToolResult{OK: true, Output: builder.String()}
		})
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (r *Registry) writeTool() ToolDef {
	return newGenericTool("write", "Crea o sobrescribe un archivo completo (crea directorios). Usa edit para cambios quirúrgicos o apply_patch para multi-archivo.",
		objectSchema(map[string]any{
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
	return newGenericTool("edit", "Reemplaza exactamente una ocurrencia de old_string por new_string. Debe aparecer exactamente 1 vez; si 0 o 2+ falla (re-lee con más contexto). Para multi-cambio usa apply_patch.",
		objectSchema(map[string]any{
			"path":       stringProp("Ruta del archivo a editar (relativa o absoluta)"),
			"old_string": stringProp("Texto exacto a reemplazar — debe aparecer exactamente 1 vez incluyendo indentación y saltos de línea"),
			"new_string": stringProp("Texto de reemplazo — mantén indentación y estilo del archivo"),
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
	return newGenericTool("glob", "Descubre archivos por patrón glob (usa ** para recursivo). Ejemplo: \"**/*.go\".",
		objectSchema(map[string]any{
			"pattern": stringProp("Patrón glob — ej: **/*.go, src/**/*.tsx, **/*.md"),
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
	return newGenericTool("grep", "Búsqueda regex en archivos (devuelve file:line:text). Usa include para filtrar por extensión. Ejemplo: {\"query\":\"func.*Health\",\"include\":\"*.go\"}.",
		objectSchema(map[string]any{
			"query":   stringProp("Patrón regex — ej: func Handle, TODO|FIXME, import.*react"),
			"path":    stringProp("Directorio raíz de búsqueda (por defecto .)"),
			"include": stringProp("Filtro glob opcional — ej: *.go, *.{ts,tsx}, *.md"),
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
	return newGenericTool("bash", "Ejecuta un comando shell y devuelve stdout/stderr + exit code. Valida tras editar (go test, npm build). Nunca uses sudo/rm -rf / — será bloqueado.",
		objectSchema(map[string]any{
			"command": stringProp("Comando shell — ej: go test ./..., npm run build, ls -la, docker ps"),
			"workdir": stringProp("Directorio de trabajo opcional (por defecto .)"),
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
	return newGenericTool("git_status", "Git status porcelain — muestra archivos modificados/no trackeados.",
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
	return newGenericTool("git_diff", "Git diff del working tree (no-stageado por defecto; staged:true para el stageado).",
		objectSchema(map[string]any{
			"staged": boolProp("Si true, diff stageado (git diff --staged)"),
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

type applyPatchArgs struct {
	Patch string `json:"patch"`
}

func (r *Registry) applyPatchTool() ToolDef {
	return newGenericTool("apply_patch", "Aplica un patch unificado al workspace. Soporta diff estándar y formato Codex \"*** Begin Patch\". Verifica con git_diff tras aplicar.",
		objectSchema(map[string]any{
			"patch": stringProp("Patch unified diff o Codex *** Begin Patch — incluir headers *** Update File / *** Add File y hunks @@"),
		}, "patch"),
		func(ctx context.Context, args applyPatchArgs) domain.ToolResult {
			if strings.TrimSpace(args.Patch) == "" {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("patch vacío")}
			}
			// Soporta formato '*** Begin Patch' de Codex y diff estándar
			patch := args.Patch
			if strings.Contains(patch, "*** Begin Patch") {
				return r.applyBeginPatch(ctx, patch)
			}
			// Diff estándar: escribir a temp y aplicar con git apply
			tmp, err := os.CreateTemp("", "forgen-patch-*.diff")
			if err != nil {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("crear temp: %w", err)}
			}
			defer func() { _ = os.Remove(tmp.Name()) }()
			if _, err := tmp.WriteString(patch); err != nil {
				return domain.ToolResult{OK: false, Error: err}
			}
			if err := tmp.Close(); err != nil {
				return domain.ToolResult{OK: false, Error: err}
			}
			// Verificar y aplicar
			if res, err := r.executor.Execute(ctx, fmt.Sprintf("git apply --check %q", tmp.Name()), ".", nil); err != nil || res.ExitCode != 0 {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("patch no aplica limpio: %s", res.Stderr+res.Stdout)}
			}
			res, err := r.executor.Execute(ctx, fmt.Sprintf("git apply %q", tmp.Name()), ".", nil)
			if err != nil {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("git apply: %w", err)}
			}
			if res.ExitCode != 0 {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("git apply falló: %s", res.Stderr+res.Stdout)}
			}
			out := strings.TrimSpace(res.Stdout + res.Stderr)
			if out == "" {
				out = "Patch aplicado correctamente"
			}
			return domain.ToolResult{OK: true, Output: out}
		})
}

func (r *Registry) applyBeginPatch(ctx context.Context, patch string) domain.ToolResult {
	// Formato Codex: *** Begin Patch / *** Update File: / *** End Patch
	lines := strings.Split(patch, "\n")
	var currentFile string
	var hunks []string
	var outputs []string
	flush := func() {
		if currentFile == "" || len(hunks) == 0 {
			return
		}
		// validate path inside workspace
		clean := filepath.Clean(currentFile)
		if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") || strings.Contains(clean, ".."+string(filepath.Separator)) {
			outputs = append(outputs, fmt.Sprintf("✗ %s: path fuera del workspace denegado", currentFile))
			hunks = nil
			return
		}
		content := strings.Join(hunks, "\n")
		dir := filepath.Dir(clean)
		if dir != "." && dir != "" {
			_, _ = r.executor.Execute(ctx, fmt.Sprintf("mkdir -p %q", dir), ".", nil)
		}
		if err := r.fs.Write(ctx, clean, []byte(content)); err != nil {
			outputs = append(outputs, fmt.Sprintf("✗ %s: %v", clean, err))
		} else {
			outputs = append(outputs, fmt.Sprintf("✓ %s", clean))
		}
		hunks = nil
	}
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "*** Begin Patch"), strings.HasPrefix(line, "*** End Patch"):
			continue
		case strings.HasPrefix(line, "*** Update File:"):
			flush()
			currentFile = strings.TrimSpace(strings.TrimPrefix(line, "*** Update File:"))
			hunks = []string{}
		case strings.HasPrefix(line, "*** Add File:"):
			flush()
			currentFile = strings.TrimSpace(strings.TrimPrefix(line, "*** Add File:"))
			hunks = []string{}
		case strings.HasPrefix(line, "*** Delete File:"):
			flush()
			p := filepath.Clean(strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File:")))
			if filepath.IsAbs(p) || strings.HasPrefix(p, "..") || strings.Contains(p, ".."+string(filepath.Separator)) {
				outputs = append(outputs, fmt.Sprintf("✗ %s: delete fuera del workspace denegado", p))
			} else if _, err := r.executor.Execute(ctx, fmt.Sprintf("rm -f %q", p), ".", nil); err == nil {
				outputs = append(outputs, fmt.Sprintf("✗ eliminado %s", p))
			}
			currentFile = ""
		default:
			if currentFile != "" {
				// Saltar líneas de diff header @@
				if strings.HasPrefix(line, "@@") {
					continue
				}
				// Quitar prefijo +/-
				if strings.HasPrefix(line, "+") {
					hunks = append(hunks, line[1:])
				} else if strings.HasPrefix(line, "-") {
					// línea eliminada, no añadir
				} else if strings.HasPrefix(line, " ") {
					hunks = append(hunks, line[1:])
				} else {
					hunks = append(hunks, line)
				}
			}
		}
	}
	flush()
	if len(outputs) == 0 {
		return domain.ToolResult{OK: false, Error: fmt.Errorf("patch vacío o no reconocido")}
	}
	return domain.ToolResult{OK: true, Output: strings.Join(outputs, "\n")}
}

type lsArgs struct {
	Path string `json:"path,omitempty"`
}

func (r *Registry) lsTool() ToolDef {
	return newGenericTool("ls", "Lista los archivos y directorios de una ruta (similar a 'ls -la'). WHEN_TO_USE: inspeccionar el contenido de un directorio para entender la estructura del repo sin abrir cada archivo. Ejemplo: {\"path\":\".\"} lista la raíz del workspace, {\"path\":\"src\"} lista src/. Incluye archivos ocultos.",
		objectSchema(map[string]any{
			"path": stringProp("Directorio a listar (por defecto .)"),
		}, "path"),
		func(ctx context.Context, args lsArgs) domain.ToolResult {
			path := strings.TrimSuffix(args.Path, "/")
			if path == "" {
				path = "."
			}
			seen := map[string]bool{}
			appendUnique := func(matches []string) {
				for _, m := range matches {
					if !seen[m] {
						seen[m] = true
					}
				}
			}
			appendUnique(mustGlob(ctx, r.fs, path+"/*"))
			appendUnique(mustGlob(ctx, r.fs, path+"/.*"))
			if len(seen) == 0 {
				return domain.ToolResult{OK: true, Output: "(directorio vacío)"}
			}
			names := make([]string, 0, len(seen))
			for m := range seen {
				names = append(names, m)
			}
			sort.Strings(names)
			return domain.ToolResult{OK: true, Output: strings.Join(names, "\n")}
		})
}

func mustGlob(ctx context.Context, fs ports.FileSystem, pattern string) []string {
	matches, _ := fs.Glob(ctx, pattern)
	return matches
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
