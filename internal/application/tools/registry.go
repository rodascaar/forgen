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
	registry.register(registry.readManyTool())
	registry.register(registry.writeTool())
	registry.register(registry.editTool())
	registry.register(registry.globTool())
	registry.register(registry.grepTool())
	registry.register(registry.bashTool())
	registry.register(registry.gitStatusTool())
	registry.register(registry.gitDiffTool())
	registry.register(registry.applyPatchTool())
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
	return newGenericTool("read", "Lee el contenido de un archivo (con offset/limit para paginar). WHEN_TO_USE: necesitas ver código/config antes de editar; para 2+ archivos usa read_many_files en un solo call (ahorra turnos en 9B). Ejemplo: {\"path\":\"src/app/page.tsx\"} o {\"path\":\"go.mod\",\"limit\":50}. Si el archivo no existe, el error indica ruta incorrecta — verifica con glob.",
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
	return newGenericTool("read_many_files", "Lee múltiples archivos en un solo call (batch). WHEN_TO_USE: necesitas 2+ archivos (ej: layout + page + config) — ahorra turnos críticamente en modelos 9B/12B donde cada tool call es caro. Alternativa a N llamadas read. Ejemplo: {\"paths\":[\"src/app/layout.tsx\",\"src/app/page.tsx\",\"package.json\"]}. Si un path falla, el output indica error per file pero el resto se devuelve.",
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
	return newGenericTool("write", "Crea o sobrescribe un archivo completo (crea directorios). WHEN_TO_USE: archivo nuevo o reemplazo total. Para cambios quirúrgicos usa edit; para multi-archivo usa apply_patch (GPT prefiere apply_patch, 9B prefiere edit). Ejemplo: {\"path\":\"src/app/dashboard/page.tsx\",\"content\":\"export default function Dashboard(){...}\"} — verifica luego con read y bash build.",
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
	return newGenericTool("edit", "Reemplaza EXACTAMENTE una ocurrencia de old_string por new_string. WHEN_TO_USE: fix quirúrgico de 1 bloque. Requiere old_string presente exactamente 1 vez — incluye 3-5 líneas de contexto para unicidad; si aparece 0 o 2+ veces falla y debes re-leer el archivo con más contexto. Para 2+ cambios en mismo archivo o multi-archivo usa apply_patch (GPT). Ejemplo: {\"path\":\"src/router.go\",\"old_string\":\"  if err != nil {\\n    return err\\n  }\",\"new_string\":\"  if err != nil {\\n    return fmt.Errorf(\\\"wrap: %w\\\", err)\\n  }\"}.",
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
	return newGenericTool("glob", "Descubre archivos por patrón glob. WHEN_TO_USE: antes de leer cuando no conoces la ruta exacta; para mapear estructura del repo. Usa ** para recursivo. Ejemplo: \"**/*.go\" lista Go files, \"src/**/*.{ts,tsx}\" lista TS, \"**/AGENTS.md\" encuentra configs. Si sin coincidencias, prueba patrón más amplio o verifica cwd con bash pwd.",
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
	return newGenericTool("grep", "Búsqueda regex en archivos (ripgrep-like). WHEN_TO_USE: localizar símbolos, imports, TODOs antes de read. Devuelve file:line:text. Usa include para filtrar por extensión. Ejemplo: {\"query\":\"func.*Health\",\"include\":\"*.go\"} o {\"query\":\"dashboard\",\"include\":\"*.{ts,tsx}\"}. Para búsquedas amplias, empieza con glob; si 0 resultados, simplifica regex.",
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
	return newGenericTool("bash", "Ejecuta comando shell y devuelve stdout/stderr + exit code. WHEN_TO_USE: validar tras editar (go test ./..., npm test, golangci-lint, git status), inspeccionar estado (docker ps, ls). Siempre verifica exit code; si !=0 el output contiene el error. Ejemplo: {\"command\":\"go test ./... 2>&1 | head -n 100\"} o {\"command\":\"npm run build\"}. Nunca uses sudo/rm -rf / — será bloqueado.",
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
	return newGenericTool("git_status", "Git status porcelain — muestra archivos modificados/no trackeados. WHEN_TO_USE: antes de editar para entender working tree; tras editar para confirmar cambios. Ejemplo: sin args. Si (working tree limpio), no hay cambios.",
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
	return newGenericTool("git_diff", "Git diff del working tree (no-stageado por defecto). WHEN_TO_USE: revisar cambios antes de commit o tras editar para validar patch. Ejemplo: {} para unstaged, {\"staged\":true} para staged. Usa para decidir si revertir con /undo.",
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
	return newGenericTool("apply_patch", "Aplica patch unificado al workspace. WHEN_TO_USE: cambios multi-archivo o multi-hunk revisables — preferido en GPT/Codex; en modelos 9B usa edit si no estás seguro del formato. Soporta unified diff y Codex \"*** Begin Patch\". Ejemplo unified: \"diff --git a/foo.go b/foo.go\\n@@ -1 +1 @@\\n- old\\n+ new\" o Codex: \"*** Begin Patch\\n*** Update File: src/app/page.tsx\\n@@ ...\\n*** End Patch\". Verifica con git_diff tras aplicar.",
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
		content := strings.Join(hunks, "\n")
		dir := filepath.Dir(currentFile)
		if dir != "." && dir != "" {
			_, _ = r.executor.Execute(ctx, fmt.Sprintf("mkdir -p %q", dir), ".", nil)
		}
		if err := r.fs.Write(ctx, currentFile, []byte(content)); err != nil {
			outputs = append(outputs, fmt.Sprintf("✗ %s: %v", currentFile, err))
		} else {
			outputs = append(outputs, fmt.Sprintf("✓ %s", currentFile))
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
			p := strings.TrimSpace(strings.TrimPrefix(line, "*** Delete File:"))
			if _, err := r.executor.Execute(ctx, fmt.Sprintf("rm -f %q", p), ".", nil); err == nil {
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
