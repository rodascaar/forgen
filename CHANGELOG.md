# Changelog

Todos los cambios notables de este proyecto se documentan en este archivo.
El formato sigue [Keep a Changelog](https://keepachangelog.com/es-1.1.0/) y
el proyecto adhiere a [Versionado Semántico](https://semver.org/lang/es/).

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

[0.1.0]: https://github.com/rodascaar/forgen/releases/tag/v0.1.0
