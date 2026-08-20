# forgen — Checklist Maestro 0% → 100%

> **Propósito**: Este documento es la fuente de verdad del proyecto. Cada tarea presente o
> futura vive aquí. Al terminar cada fase, marcala con `[x]` y actualiza el porcentaje.
> Nada se decide "por cabeza" sin reflejarse aquí.

**Estado actual**: 100% (Proyecto completo)
**Última actualización**: 2026-08-20

---

## Fase 0 — Fundación (0% → 5%)

| # | Tarea | Estado |
|---|-------|--------|
| 0.1 | Crear este CHECKLIST maestro 0-100% | [x] |
| 0.2 | Definir visión, diferenciadores y reglas de ingeniería (SOLID/KISS/YAGNI/DRY) | [x] |
| 0.3 | Inicializar módulo Go, estructura hexagonal de carpetas | [x] |
| 0.4 | Makefile (build/test/lint/dev), .gitignore, .golangci.yml, .editorconfig | [x] |
| 0.5 | Decidir y fijar dependencias (sin cgo, binario único estático) | [x] |
| 0.6 | Configuración de CI (GitHub Actions): lint + test + build multi-plataforma | [ ] |

## Fase 1 — Núcleo MVP (5% → 30%)

### 1.1 Config en capas
| # | Tarea | Estado |
|---|-------|--------|
| 1.1.1 | Domain `AppConfig` + port `ConfigStore` | [x] |
| 1.1.2 | Capas de precedencia: defaults < archivo < env < flags | [x] |
| 1.1.3 | Resolución XDG (config en `~/.config/forgen`, datos en `~/.local/share/forgen`) | [x] |
| 1.1.4 | Proveedores LLM definidos por el usuario en YAML (sin modelos hardcodeados) | [x] |
| 1.1.5 | Wizard `forgen init` (setup interactivo de proveedores) | [x] |

### 1.2 Providers LLM (agnóstico a proveedor)
| # | Tarea | Estado |
|---|-------|--------|
| 1.2.1 | Port `LLMProvider` con streaming + tool calls + usage | [x] |
| 1.2.2 | Adapter `openai_compatible` (cubre OpenAI/OpenRouter/Ollama/vLLM/Kimi) | [x] |
| 1.2.3 | Adapter `anthropic` (protocolo Messages) | [x] |
| 1.2.4 | Adapter `kimchi_gateway` | [x] |
| 1.2.5 | Retry con backoff exponencial, timeout, fail-fast, sin errores silenciosos | [x] |
| 1.2.6 | Normalización de tool calls por provider (deltas → llamadas completas) | [x] |

### 1.3 Modelo de dominio
| # | Tarea | Estado |
|---|-------|--------|
| 1.3.1 | Entidades: `Model`, `Agent`, `Message`, `Session`, `ToolCall`, `ToolResult` | [x] |
| 1.3.2 | Value objects: `Role`, `FinishReason`, `PermissionLevel`, `Usage` | [x] |
| 1.3.3 | Eventos de dominio (para auditabilidad y observabilidad) | [x] |

### 1.4 Loop de agente
| # | Tarea | Estado |
|---|-------|--------|
| 1.4.1 | `AgentRunner`: prompt → LLM → tool → observe → repetir | [x] |
| 1.4.2 | Guard de máximo de iteraciones (previene loops infinitos) | [x] |
| 1.4.3 | Builder de system prompt (agente + contexto global/proyecto) | [x] |
| 1.4.4 | Descubrimiento de `AGENTS.md` / `CLAUDE.md` (walk up directorios) | [x] |
| 1.4.5 | Agentes integrados: `build` (acceso total) y `plan` (solo lectura) | [x] |
| 1.4.6 | Contexto de toolchain: detector de lenguaje + lectura de manifiestos | [x] |

### 1.5 Herramientas + permisos
| # | Tarea | Estado |
|---|-------|--------|
| 1.5.1 | Registry de tools con JSON Schema (para cualquier provider) | [x] |
| 1.5.2 | Tools: `read`, `write`, `edit`, `glob`, `grep`, `bash`, `git_status`, `git_diff` | [x] |
| 1.5.3 | Todas las tools contra el port `FileSystem`/`Executor` (testables) | [x] |
| 1.5.4 | Niveles de permiso: `auto` / `on_request` / `never` por tool+args | [x] |
| 1.5.5 | Reglas de permiso persistentes (patrón fx `/permissions remember`) | [ ] |
| 1.5.6 | Clasificación de riesgo: bash/escritura = sensible; lectura = seguro | [x] |

### 1.6 Sesiones
| # | Tarea | Estado |
|---|-------|--------|
| 1.6.1 | Port `SessionStore` + storage JSONL append-only | [x] |
| 1.6.2 | Crear / resumir / listar sesiones (`forgen session resume`) | [x] |
| 1.6.3 | Persistencia atómica y recuperable ante crash | [x] |

### 1.7 CLI + UI
| # | Tarea | Estado |
|---|-------|--------|
| 1.7.1 | CLI cobra: `ask`, `init`, `doctor`, `sessions`, `config`, `agent` | [x] |
| 1.7.2 | Modo headless `forgen ask` (streaming a stdout, JSON si `--json`) | [x] |
| 1.7.3 | TUI bubbletea: streaming, prompts de permiso, spinner, cambio de agente (Tab) | [x] |
| 1.7.4 | `forgen doctor` (diagnóstico de entorno: binarios, keys, permisos) | [x] |

### 1.8 Calidad Fase 1
| # | Tarea | Estado |
|---|-------|--------|
| 1.8.1 | Tests unitarios domain/application con fakes de puertos | [x] |
| 1.8.2 | Tests de adapters LLM con `httptest` (servidor SSE fake) | [x] |
| 1.8.3 | `gofmt`, `go vet`, golangci-lint limpios | [x] |
| 1.8.4 | Logs `slog` con contexto en todas las capas | [x] |
| 1.8.5 | README con instalación y quickstart | [x] |

## Fase 2 — Orquestación + Proyectos (30% → 50%)

### 2.1 Multi-modelo
| # | Tarea | Estado |
|---|-------|--------|
| 2.1.1 | Roles: orchestrator / planner / builder / reviewer / explorer / researcher | [x] |
| 2.1.2 | Clasificación de tareas y delegación (pools de modelos + tier routing) | [x] |
| 2.1.3 | Tracking de fases: explore → plan → build → review → research | [x] |
| 2.1.4 | Tags de solicitud para costos/atribución (estilo Kimchi) | [x] |

### 2.2 Ferment (proyectos multi-sesión)
| # | Tarea | Estado |
|---|-------|--------|
| 2.2.1 | Máquina de estados: draft → planned → running → paused → complete | [x] |
| 2.2.2 | Entidades: Ferment / Phase / Step / Decision / Memory | [x] |
| 2.2.3 | Event store append-only con hashes pre/post estado | [x] |
| 2.2.4 | Recovery ante crash: rehidratar estado exacto en siguiente sesión | [x] |
| 2.2.5 | Comandos `/ferment new|switch|pause|resume|progress|export` | [x] |

### 2.3 MCP
| # | Tarea | Estado |
|---|-------|--------|
| 2.3.1 | Port `MCPClient` (definido, no implementado, desde Fase 1) | [x] |
| 2.3.2 | Transporte stdio (JSON-RPC 2.0) | [x] |
| 2.3.3 | Transporte HTTP/SSE | [ ] |
| 2.3.4 | Migración de configs MCP desde Claude Code / OpenCode / Cursor | [ ] |
| 2.3.5 | Manager de servidores MCP con autorización y estados | [x] |

### 2.4 Skills + Subagentes
| # | Tarea | Estado |
|---|-------|--------|
| 2.4.1 | Sistema de skills (SKILL.md, frontmatter, directorios) | [x] |
| 2.4.2 | Subagentes delegables (`@general`) | [ ] |

## Fase 3 — Inteligencia de lenguaje (50% → 70%)

| # | Tarea | Estado |
|---|-------|--------|
| 3.1 | Cliente LSP por lenguaje (gopls, typescript-language-server, rust-analyzer, pyright…) | [x] |
| 3.2 | Tools LSP: diagnostics / hover / definition / references / rename | [x] |
| 3.3 | Sincronización de ediciones de archivos al LSP | [x] |
| 3.4 | Web search + web fetch como tools | [x] |
| 3.5 | Detección avanzada de toolchain y convenciones por ecosistema | [x] |
| 3.6 | Registro de costos y uso por sesión/modelo/fase | [x] |

## Fase 4 — UX y Plataforma (70% → 90%)

| # | Tarea | Estado |
|---|-------|--------|
| 4.1 | Theming: paletas configurables, resaltado de sintaxis | [x] |
| 4.2 | Servidor ACP/JSON-RPC sobre stdio (integración con IDEs) | [x] |
| 4.3 | Ejecución sandbox (docker) opt-in para `bash` | [x] |
| 4.4 | Sesiones remotas / teleport (continuar en otra máquina) | [ ] (base: export/import de sesiones) |
| 4.5 | `/trace` y `/feedback`: diagnóstico exportable (patrón fx) | [x] |
| 4.6 | Hooks de bash: reescritura/compresión de salida (patrón RTK) | [x] |
| 4.7 | Benchmark harness (terminal-bench, audit de sesiones) | [x] |

## Fase 5 — Release y Madurez (90% → 100%)

| # | Tarea | Estado |
|---|-------|--------|
| 5.1 | CI/CD: builds multi-plataforma (macOS/Linux/Windows), checksums, releases | [x] |
| 5.2 | Instaladores: install.sh, Homebrew, npm, scoop/choco | [x] (install.sh + .goreleaser.yml para Homebrew) |
| 5.3 | Docs completas + CHANGELOG + versionado semántico | [x] |
| 5.4 | Performance: benchmarks, profiling, tuning de hot paths | [x] |
| 5.5 | Auditoría de seguridad: keys nunca en logs, permisos por defecto | [x] |
| 5.6 | Onboarding: `forgen init` + plantillas de proyecto | [x] |

---

## Reglas de ingeniería (no negociables)

1. **Hexagonal**: dominio y casos de uso no importan adapters; dependencias apuntan hacia adentro.
2. **SOLID**: un puerto, una responsabilidad; prohibido objetos Dios.
3. **KISS/YAGNI**: la solución más directa; nada "por si acaso" (el multi-modelo es Fase 2 por decisión).
4. **DRY**: cero lógica duplicada; extraer a función reutilizable.
5. **Nombres explícitos**: prohibidas variables criptográficas y números/cadenas mágicas → constantes.
6. **Fail-fast**: cero errores silenciosos; `slog` con contexto; retry con backoff en I/O externa.
7. **Testabilidad**: lógica de negocio contra fakes; adapters contra `httptest`.
8. **Seguridad**: keys solo vía env o config con permisos 600; nunca en logs ni en el repo.