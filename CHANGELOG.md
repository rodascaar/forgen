# Changelog

Todos los cambios notables de este proyecto se documentan en este archivo.
El formato sigue [Keep a Changelog](https://keepachangelog.com/es-1.1.0/) y
el proyecto adhiere a [Versionado Semántico](https://semver.org/lang/es/).

## [0.1.21] - 2026-08-25

### Añadido

- **Transcript jerárquico (TUI pulido)** — `internal/adapters/in/tui/model.go`:
  - **Separadores de turno**: línea divisoria `─────` al iniciar cada prompt del usuario y al terminar cada respuesta del asistente, para ver claro dónde empieza/termina cada turno (con guard anti-divisores consecutivos).
  - **Jerarquía de pasos**: los resultados de herramientas (`✓/✗ tool output`) se muestran **indentados** bajo su tool call (`▶ tool`), agrupando acción + resultado como un paso.
  - **Colapso de respuestas largas**: respuestas del asistente >500 caracteres se colapsan a un header `▸ Respuesta (N líneas)` y se expanden con `Ctrl+O` (o `Alt+O`), reduciendo el scroll en sesiones largas.
  - Atajo `Ctrl+O` documentado en `/help`.

## [0.1.20] - 2026-08-25

### Corregido

- **Test `TestOutsideWorkspacePromptsInAuto` fallaba en Windows (CI -race)**: el literal `/etc/passwd` no es ruta absoluta en Windows (`filepath.IsAbs` exige drive letter), así que `Join(ws, "/etc/passwd")` quedaba dentro del workspace y el test fallaba. Ahora el test usa una ruta absoluta real fuera del workspace (`filepath.Join(t.TempDir(), ...)`) cross-platform (`internal/application/permission/service_test.go`).

## [0.1.19] - 2026-08-25

### Corregido

- **Bug: "ruta fuera del workspace" rompía el system prompt**: el boundary de contención se había aplicado al `FileSystem` base (`internal/adapters/out/fs/os.go`), lo que rompía el walk-up de `AGENTS.md`/`CLAUDE.md` a directorios padres de `LoadProjectContext` (daba `verificar .../AGENTS.md: ruta fuera del workspace`). Se revierte: el `FileSystem` vuelve a `resolve` original y la contención vive en la capa de permisos del agente (patrón opencode `external_directory` / codex `writable_roots`).

### Añadido

- **Contención en `permission.Service`** (`internal/application/permission/service.go`) — solo pregunta en casos de riesgo, archivos normales dentro del workspace nunca preguntan:
  - `isOutsideWorkspace`: lectura/escritura fuera del `cwd` (`../`, absoluta, `~`, glob) → confirmación.
  - `isDatabaseWrite`: escribir/editar archivos `.db/.sqlite/.sqlite3/.mdb/.db-wal/.db-shm` → confirmación.
  - `dangerousSQLPatterns`: `DELETE FROM`, `DROP`, `TRUNCATE`, `DELETE/UPDATE WHERE 1=1` en bash (sqlite3/psql/mysql) → confirmación.
  - `dangerousPatterns` ampliado: `rm -f/-r/--force`, `rm/rmdir/unlink/del` sobre rutas sistema/absolutas/home, `kill -9` (patrón codex `is_dangerous_command`).

## [0.1.18] - 2026-08-25

### Añadido

- **Seguridad lectura + escape workspace**: lectura de ficheros sensibles (`.env`, `*.pem`, `*.key`, `~/.aws`, `.ssh`, `id_rsa`, `credentials`) ahora pide confirmación incluso en modo `auto` (`internal/application/permission/service.go:isSensitiveRead`). `read`/`glob`/`grep`/`read_many_files` con `../` o ruta absoluta fuera del workspace fallan con `ruta fuera del workspace` (`internal/adapters/out/fs/os.go:safeResolve` — boundary check replicado de `checkpoint.go`).
- **Confirmación Y/N/A**: el prompt de permiso ahora ofrece `y` permitir, `n` denegar y `a` **permitir siempre** (persiste regla Auto en la sesión vía `permission.Service.AddRule`). TUI (`internal/adapters/in/tui/model.go`), CLI headless (`internal/adapters/in/cli/messenger.go`) y `PermissionChoice` en dominio (`internal/core/domain/permission.go`).

## [0.1.17] - 2026-08-25

### Corregido

- **CI Lint (10 issues)**: `memory/service.go` permisos 0600 + errcheck + `G703` guard, `permission` rama vacía, `plan` `fmt.Fprintf`, `prealloc` en `runner`/`compaction`, `unused` `executeWithPermission`/`isComplex` (`nolint`), `task` `context.WithoutCancel` para background (`internal/application/task/tools.go:52`).

## [0.1.16] - 2026-08-25

### Añadido

- **Harness agnóstico 9B→gigante (Fase 7.1-7.6)**: compaction estándar 2-step Extract (prune no-destructivo 40k/20k + LLM 5 headings, `internal/application/session/compaction.go`), prompts bilingües `build/plan` 2-4k `es/en` (`internal/core/domain/prompts/*.md`, `FORGEN_LANG`), tuning por familia (`apply_patch` GPT vs `edit` 9B), `AGENTS.md` bilingüe walk-up, tool schemas ricos con `WHEN_TO_USE` + ejemplos, `read_many_files` batch (9B), `update_plan` FSM 1-7 pasos con artifact `.forgen/plans/plan.md`, parallel tool calls 5 + doom-loop guard, `.gitignore` respect, RepoMap MCP docs, subagentes fresh window + tier routing (light/standard/heavy scoring), `run_in_background`, `PreToolUse` block `.env/secrets`, `PostToolUse` LSP diagnostics auto, `LSP.DiagnosticsFor`, memoria `.forgen/memory.md` + skills budget 25k/5k LIFO + token budget warn, CLI `forgen compact/context` y TUI `/compact /context` (`internal/app/app.go`, `internal/application/agent/runner.go`, `internal/application/permission/service.go`, `internal/application/lsp/manager.go`, `internal/application/memory/service.go`).
- **Compaction threshold configurable**: `compaction.threshold/disabled` en `config.yaml`, `FORGEN_DISABLE_AUTOCOMPACT`, `ModelMetadata.context_limit/max_output` para `isOverflow` por modelo (`internal/core/domain/config.go`, `orchestration.go`).

## [0.1.15] - 2026-08-24

### Añadido

- **Crear sesión nueva**: comando `/new` en la TUI y opción "＋ Nueva sesión" al final del picker `/sessions`; también `forgen sessions new` en el CLI. Una vez activa una sesión ya se puede empezar una limpia sin reiniciar.
- **Git interno vs git del usuario**: tracking propio de forgen por snapshots del workspace (`FORGEN_DATA_DIR/workspaces/`). `git_status`/`git_diff` siempre funcionan: si el proyecto es un repo git real usan ese; si no, usan el versionado interno — el agente ya no ve errores en rojo cuando el cliente no inicializó git, y puede razonar/revertir cambios con `/undo`.

### Corregido

- **`git_status`/`git_diff` fallaban en workspaces sin repo git**: ahora el adapter combinado delega al git real si `IsRepo`, y al tracking interno si no; nunca depende de que el usuario configure git.

## [0.1.14] - 2026-08-24

### Añadido

- **Razonamiento por niveles (off|low|medium|high)**: configuración `AppConfig.reasoning_effort`, flag `--reasoning` en `forgen ask` y comando `/reasoning` en la TUI (con indicador en la barra de estado). Se envía `reasoning_effort` en proveedores OpenAI-compatible (DeepSeek/OpenAI/NVIDIA/Qwen) y `thinking.budget_tokens` en Anthropic.
- **Copiar salida**: `/copy` copia la última respuesta del asistente y `/copy all` todo el transcript al portapapeles (la captura del ratón impedía seleccionar con Cmd+C).
- **Reanudar sesiones de verdad**: al elegir sesión (`/sessions` o `/resume <id>`) se cargan sus mensajes en el transcript para ver y continuar la conversación.

### Corregido

- **Cambiar de modelo no surtía efecto hasta reiniciar**: el modelo usado por el runner salía de `session.Model` (antiguo) al reanudar; ahora `runAgent`/`runAsk` sincronizan `session.Model` con el modelo resuelto, aplicando el cambio en el mismo turno.

## [0.1.13] - 2026-08-24

### Añadido

- **Rollback interno (checkpoints)**: snapshot del workspace antes de cada run de build (`FORGEN_DATA_DIR/checkpoints/<session>/<run>/`), con `/undo` en la TUI y `forgen undo` / `forgen checkpoints list` en la CLI para revertir iteraciones fallidas sin depender de Git manual.
- **Banner FORGEN generado con go-figure** (fuente "block" solo ASCII): elimina los artefactos/desalineación de los caracteres box-drawing.

### Cambiado

- **Modo plan (red de seguridad)**: guard a nivel de ejecución en el runner — un agente de solo lectura nunca ejecuta una herramienta fuera del allowlist, aunque el LLM la pidiera.
- **Control de procesos**: los comandos (`docker compose up`, etc.) se ejecutan en su propio grupo de procesos y, al cancelar, se mata todo el árbol (SIGTERM→SIGKILL). Ctrl+C es idempotente (una sola vez, sin acumular "Cancelando petición...").
- **Spinner acoplado al ciclo de vida**: timeout por llamada LLM (150s) y timeout global del turno (10min) para que el spinner se apague siempre; se garantiza `runDoneMsg` en cada finalización/cancelación.
- **Prompts (Fase 2)**: el agente build ahora exige aplicar TODOS los requisitos (no solo nombres) y comprobar el estado actual (docker ps, git status) antes de arrancar servicios para evitar redundancia/bloqueos.

## [0.1.12] - 2026-08-24

### Añadido

- **Modo plan recomienda la mejor opción**: el agente plan presenta 2-3 enfoques con tradeoffs y marca la recomendación con `✅ Recomendación:` justificada con la evidencia investigada. En la TUI esa línea se resalta en el color de marca (`internal/core/domain/session.go`, `internal/adapters/in/tui/model.go`).

### Corregido

- **N del logotipo FORGEN**: se restaura la N auténtica de la fuente block (11 celdas), eliminando la distorsión que introducía la versión comprimida a 8 celdas.

## [0.1.11] - 2026-08-24

### Corregido

- **Banner FORGEN**: el logotipo ahora usa un color de marca fijo `#A6D93B` (TrueColor `38;2;166;217;59`) independiente del tema del usuario — antes salía lila/morado porque tomaba `accent` del config (`#bb9af7`). Se renderiza línea por línea con RESET `\x1b[0m` al final de cada una (sin fugas de color) y el bloque ASCII quedó con ancho uniforme (8 celdas por glifo) para eliminar la desalineación ("dientes").

## [0.1.10] - 2026-08-24

### Añadido

- **Identidad FORGEN (ASCII block)**: logotipo FORGEN en color de marca Lima ácida (`#A6D93B`) en el banner de inicio de la TUI y en la pantalla de ayuda (`internal/adapters/in/tui/model.go`).

### Cambiado

- **Modo plan estrictamente de solo lectura**: `visibleTools` ahora usa un allowlist de herramientas de lectura/exploración (`read`, `glob`, `grep`, `git_status`, `git_diff`, `read_skill`, `web_fetch`, `web_search`, LSP de lectura). El agente plan ya no puede `bash`, `write`, `edit`, `apply_patch`, `task` (sub-agentes build), `lsp_rename`, `todo` ni `mcp_*`. `DeniedTools` ampliado y `SystemPrompt` aclarado en `internal/core/domain/session.go`.

### Corregido

- **El modo plan ya no construye/modifica**: antes podía lanzar sub-agentes `build` vía `task` o aplicar cambios con `apply_patch`/`lsp_rename`; ahora solo puede investigar (leer logs, buscar en la web) y devolver un plan.

## [0.1.9] - 2026-08-24

### Añadido

- **Proveedor NVIDIA NIM**: preset `nvidia` (OpenAI-compatible, `https://integrate.api.nvidia.com/v1`, `NVIDIA_API_KEY`) con modelos `nvidia/llama-3.1-nemotron-ultra-253b-v1` y `meta/llama-3.3-70b-instruct` (`internal/core/domain/preset.go`). Uso: `forgen provider add nvidia` o `/init` → `nvidia`.

## [0.1.8] - 2026-08-24

### Añadido

- **Scroll por ratón/trackpad**: rueda del ratón desplaza la conversación (`tea.WithMouseCellMotion` + `MouseButtonWheelUp/Down` en `internal/adapters/in/tui/model.go`), como en Claude Code/opencode.
- **Color de marca (Lima ácida `#A6D93B`)**: `Accent` del tema por defecto y línea de estado rediseñada como prompt estilo shell `~/proj $ build` en el color de marca; la barra de escritura ahora tiene borde de marca y queda siempre anclada al fondo.

### Cambiado

- **Se liberan las teclas de edición**: se quitan los atajos `Ctrl` que chocaban con la edición estándar de VS Code/macOS/readline (`Ctrl+U/D/H/P/M/Q` y variantes `Alt`). Ahora `Ctrl+U/D/H/W/A/E…` funcionan dentro del campo; la navegación es `PgUp/PgDn` + ratón y todo el control de la app va por `/comandos` (`/todo`, `/mcp`, `/help`, `/quit`). `Ctrl+C` sigue cancelando/saliendo (doble pulso).

### Corregido

- **Scroll del transcript**: la ventana visible ahora devuelve exactamente `limit` líneas (`wrapped[start:start+limit]`), de modo que la barra de estado y el input ya no se empujan fuera de pantalla al subir; al terminar una petición el scroll vuelve al fondo.

## [0.1.0] - 2026-08-20

### Añadido

- **Núcleo (Fase 1)**: arquitectura hexagonal, agentes `build`/`plan`, loop de
  agente con streaming, 8 herramientas agnósticas al lenguaje, permisos con
  detección de comandos destructivos, sesiones JSONL resumibles, CLI cobra y
  TUI (bubbletea).
- **Proveedores LLM sin lock-in**: `openai_compatible`, `anthropic`, `kimchi`.
- **Orquestación (Fase 2)**: roles por fase, routing por tier, tags de costos.
- **Ferment (Fase 2)**: proyectos multi-sesión con máquina de estados y event
  store append-only con hash encadenado.
- **Skills y MCP (Fase 2)**: descubrimiento de `SKILL.md`, `read_skill`, y
  cliente MCP stdio (JSON-RPC).
- **Inteligencia de lenguaje (Fase 3)**: LSP (diagnostics/hover/definition/
  references/rename) con sincronización de ediciones; `web_fetch`/`web_search`;
  detección de toolchain con convenciones; registro de uso/costos.
- **UX y plataforma (Fase 4)**: theming, `forgen trace`, hooks de bash, sandbox
  docker, servidor JSON-RPC (`forgen serve`), auditoría y export/import de
  sesiones.
- **Release (Fase 5)**: CI multi-plataforma, GoReleaser, instalador `install.sh`,
  benchmarks y escaneo de seguridad (gitleaks + govulncheck).

## [0.1.6] - 2026-08-24

### Añadido

- **MCP HTTP/SSE** (`type: http|sse` con `url/headers`) + `forgen mcp list/add/remove/test/migrate` y migración desde Claude Code/OpenCode/Cursor (`internal/adapters/out/mcp/http.go`, `internal/application/mcp/migrate.go`); wildcards `mcp_*` en permisos; `forgen doctor` reporta MCP/LSP.
- **Subagentes orquestados** con `RunnerFactory` aislado por tipo (`internal/adapters/out/task/executor.go` + `internal/app/app.go:179`), `LoadSubAgentRegistry` desde `.forgen/agents/<type>.md` frontmatter, y `docs/ARCHITECTURE.md` con diagrama harness.
- **Permisos `remember` fx-style** (`forgen permissions remember/revoke`, stable sha256, `internal/adapters/in/cli/permissions.go`) y tests `http_test.go` + `service_wildcard_test.go`.

## [0.1.7] - 2026-08-24

### Añadido

- **README práctico (Standard Readme)**: reescrito para responder ¿qué es?/¿para qué sirve?/¿cómo lo uso ahora? con badges, TOC, Quick Start copy-paste y sin referencias a implementaciones internas.

### Cambiado

- **TUI modo cuchara**: ninguna letra sola abre menús — `q`/`p`/`m`/`?` ya escriben siempre. Atajos ahora requieren `Ctrl` (`Ctrl+P` plan, `Ctrl+M` mcp, `Ctrl+H` ayuda, `Ctrl+U/D` scroll) con alias `Alt` para `Cmd` en Mac; `Tab` sigue cambiando `build↔plan`; salida requiere doble `Ctrl+C`/`Ctrl+Q` con `Esc` cancela y footer siempre visible.

### Corregido

- **CI Lint `S1023`**: `redundant break` en `case "esc"` de `internal/adapters/in/tui/model.go:550`.

## [No liberado]

### Añadido

- **`forgen upgrade`**: auto-actualización desde GitHub Releases (`--check` para
  comprobar sin tocar nada, `-y` para omitir confirmación) y `forgen version`.
- **UX de la TUI**: slash commands dentro de la interfaz interactiva (`/init`,
  `/provider`, `/model`, `/sessions`, `/help`, `/quit`), overlay de ayuda con
  `?`, word-wrap y scroll con `PgUp/PgDn`.
- **Onboarding guiado**: detección de primer uso con banner accionable y
  asistente `/init` que configura proveedor + API key sin salir del programa.
- **Selector de proveedor/modelo** (`/provider`, `/model`) con listado en vivo
  de los modelos de tu cuenta (estilo opencode/fx).

### Corregido

- **No se podía escribir en la TUI**: `Init()` con receiver por valor descartaba
  `input.Focus()`, dejando el campo sin foco; ahora el input se enfoca al
  construir el modelo.
- **Pantalla en blanco al primer arranque**: los mensajes de onboarding que
  `Init()` descartaba por receiver por valor ahora persisten.
- **Prompt de permisos**: la tecla `?` muestra el comando completo antes de
  aprobar/denegar.
- **`forgen ask`**: avisa de forma clara cuando falta la API key en vez de
  fallar con un error de autenticación críptico.
- **Ruido del Tab**: cambiar de agente ya no llena el transcript con avisos.
- **Congelamiento de la TUI al confirmar en `/init`, `/provider`, `/model` y
  `/sessions`**: los mensajes que cierran el asistente/selector se delegaban al
  propio sub-modelo y se tragaban, dejando la UI sin salida; ahora se procesan
  antes de delegar.
- **Modal de permisos atascado**: si la petición terminaba o fallaba con un
  permiso pendiente, la UI quedaba en "¿Permitir...?" sin poder escribir; ahora
  el estado de confirmación se resetea al finalizar la petición.
- **Proveedores locales sin API key (Ollama)**: ya no se marcan como "sin
  configurar"; se consideran usables si el endpoint es local.
- **Validación/listado de modelos con timeout (15s)**: el paso "Validando..." y
  el listado en vivo de `/model` no se quedan colgados si el proveedor tarda.

[0.1.14]: https://github.com/rodascaar/forgen/releases/tag/v0.1.14
[0.1.13]: https://github.com/rodascaar/forgen/releases/tag/v0.1.13
[0.1.12]: https://github.com/rodascaar/forgen/releases/tag/v0.1.12
[0.1.11]: https://github.com/rodascaar/forgen/releases/tag/v0.1.11
[0.1.10]: https://github.com/rodascaar/forgen/releases/tag/v0.1.10
[0.1.9]: https://github.com/rodascaar/forgen/releases/tag/v0.1.9
[0.1.8]: https://github.com/rodascaar/forgen/releases/tag/v0.1.8
[0.1.7]: https://github.com/rodascaar/forgen/releases/tag/v0.1.7
[0.1.6]: https://github.com/rodascaar/forgen/releases/tag/v0.1.6
[0.1.0]: https://github.com/rodascaar/forgen/releases/tag/v0.1.0
