# forgen

> Agente de código en tu terminal. Escribe en lenguaje natural, forgen lee, edita y ejecuta — con el LLM y el lenguaje que ya usas.

[![Go Version](https://img.shields.io/badge/go-1.22%20toolchain%201.23-00ADD8?style=flat&logo=go)](go.mod)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue?style=flat)](LICENSE)
[![Build](https://img.shields.io/badge/build-passing-brightgreen?style=flat)](#tests)

**¿Qué es?** Un binario único en Go, sin dependencias, que lleva un agente autónomo a tu repo local.
**¿Para qué sirve?** Explicar código, crear features, hacer refactors, correr tests y generar PRs sin salir del terminal.
**¿Cómo lo uso ahora?** `forgen` → describe tu tarea → `Enter`. Ver [Quick Start](#quick-start).

## Tabla de contenidos

- [Instalación](#instalación)
- [Quick Start](#quick-start)
- [Uso](#uso)
- [Configuración](#configuración)
- [Ejemplos](#ejemplos)
- [Herramientas y capacidades](#herramientas-y-capacidades)
- [Contribuir](#contribuir)
- [Tests](#tests)
- [Licencia](#licencia)

## Instalación

**Requisitos:** Go 1.22+, Git, y una API key de tu proveedor LLM.

```bash
git clone https://github.com/rodascaar/forgen.git
cd forgen
go build -o bin/forgen ./cmd/forgen
# opcional: instalar en el PATH
go install ./cmd/forgen
# verificar
forgen version
forgen doctor
```

¿Sin Go? Descarga el binario de [Releases](https://github.com/rodascaar/forgen/releases) y ponlo en tu `PATH`.

## Quick Start

Todo funciona con comandos copy-paste. 60 segundos para estar productivo:

```bash
# 1. Configura tu LLM (wizard interactivo, la key queda en el keychain del SO)
forgen init
# alternativa directa:
forgen auth              # te pide solo la API key y detecta tus modelos
forgen provider list     # ver proveedores disponibles

# 2. Abre la TUI
forgen
# Dentro: escribe tu tarea y pulsa Enter
# > añade un endpoint GET /health que devuelva {status:"ok"}

# 3. Sin TUI (headless, ideal para CI)
forgen ask "explica este repo en 5 bullets"
forgen ask --json "lista los TODOs y propone un plan" > out.jsonl
```

Primera vez sin configurar: `forgen` te muestra `Escribe /init para empezar` — no necesitas leer docs.

## Uso

### CLI

```bash
forgen                              # TUI interactiva
forgen ask "tu prompt"              # una petición headless
forgen ask --session <id> "sigue"   # continuar sesión
forgen sessions                     # listar sesiones guardadas
forgen sessions resume <id>         # ver sesión
forgen sessions new                 # crear una sesión nueva
forgen config                       # config efectiva
forgen provider list                # proveedores y modelos
forgen provider add openai          # añadir proveedor
forgen mcp list                     # servidores MCP
forgen usage                        # consumo tokens
forgen upgrade --check              # comprobar actualización
```

### Dentro de la TUI

Escribir es siempre seguro: **ninguna letra sola abre menús**. Todo es `/comando` o `Ctrl+atajo`.

| Comando | Acción |
|---------|--------|
| `/init` | Configura proveedor y API key |
| `/search` | Brave search API key |
| `/provider` | Cambia proveedor por defecto |
| `/model` | Cambia modelo (listado en vivo) |
| `/sessions` | Retoma sesión guardada |
| `/new` | Inicia una sesión nueva |
| `/resume` | Reanuda una sesión por ID |
| `/todo` `/plan` | Ver lista de tareas |
| `/task` | Ver sub-agentes |
| `/mcp` | Ver servidores MCP |
| `/orchestration` `/orch` | Routing multi-modelo |
| `/diff` `/commit` | Ver cambios git |
| `/review` `/test` `/lint` `/fix` | Delegar a sub-agente |
| `/pr` | Crear PR (gh) |
| `/compact` | Compactar contexto |
| `/context` | Estado de tokens |
| `/trace` | Diagnóstico modelo/context |
| `/undo` | Revertir última iteración |
| `/retry` | Reintentar último prompt |
| `/reasoning` `/reason` | Nivel reasoning |
| `/copy` | Copiar al portapapeles |
| `/help` `/?` | Ayuda |
| `/quit` `/exit` | Salir |

**Atajos (estándar global):**

`Enter` envía · `Tab` cambia agente `build↔plan` · `Ctrl+H` ayuda · `Ctrl+P` plan · `Ctrl+M` MCP · `PgUp/PgDn` o `Ctrl+U/D` scroll · `Ctrl+C` cancela · `Ctrl+Q` o `Ctrl+C` dos veces para salir ( `Esc` cancela).

Footer siempre visible: `Ctrl+P plan · Ctrl+M mcp · Ctrl+H ayuda · / comandos · Tab agente`.

## Configuración

Config vive en `~/.config/forgen/config.yaml` (XDG). Precedencia: `defaults < archivo < env FORGEN_* < flags`.

```yaml
providers:
  - name: openai
    type: openai_compatible
    base_url: https://api.openai.com/v1
    api_key_env: OPENAI_API_KEY
    models: [gpt-5, gpt-5-mini]
  - name: local
    type: openai_compatible
    base_url: http://localhost:11434/v1
    api_key_env: ""
    models: [llama3]
default:
  provider: openai
  model: gpt-5
permissions:
  mode: auto        # auto | on_request | never
agent: build        # build | plan
theme:
  accent: "#7aa4ff"
```

**Proveedores soportados:** cualquier API `OpenAI-compatible` (OpenAI, OpenRouter, Groq, Together, Mistral, Ollama local, etc.) y Anthropic nativo. `forgen provider list` muestra presets; `forgen auth` guarda tu key en el keychain ( `0600` fallback ) y auto-detecta modelos.

Variables útiles: `FORGEN_CONFIG_DIR`, `FORGEN_DATA_DIR`, `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `BRAVE_API_KEY` (búsqueda).

## Ejemplos

```bash
# Planificar antes de codificar
forgen
> /plan
> diseña un plan para añadir autenticación JWT, no codifiques aún — solo el plan

# Revisar tu diff actual
forgen
> /review

# Fix automático de tests/lint
forgen
> /fix

# Headless en CI
forgen ask --json "ejecuta go test ./... y resume fallos" | jq

# MCP: darle herramientas externas al agente
forgen mcp add filesystem --type stdio --command npx --args "-y,@modelcontextprotocol/server-filesystem,/tmp"
forgen mcp test filesystem
```

## Herramientas y capacidades

- **Edición segura:** `read` `write` `edit` `glob` `grep` `bash` `git_status` `git_diff` — sobre puertos inyectables, testeables sin tocar disco.
- **Git interno vs git del usuario:** forgen mantiene un versionado propio por snapshots del workspace (`~/.local/share/forgen/workspaces/`). `git_status`/`git_diff` **siempre funcionan**: si el proyecto es un repo git real usan ese; si no, usan el tracking interno — así el agente nunca ve errores en rojo y puede razonar/revertir cambios con `/undo` sin depender de que hayas configurado git.
- **LSP opcional:** si `gopls`, `typescript-language-server`, `rust-analyzer` o `pyright` están instalados, forgen usa `lsp_diagnostics` `lsp_hover` `lsp_definition` `lsp_references` `lsp_rename`.
- **MCP:** `stdio` / `http` / `sse`. Tools como `<server>_<tool>`. `forgen mcp add <nombre> --type http --url https://...` · `forgen mcp migrate` importa servidores existentes sin sobrescribir.
- **Búsqueda web:** `web_search` (Brave) + `web_fetch` (extrae texto de URL). Config: `search.provider: brave`.
- **Proyectos multi-sesión:** `forgen ferment new "Build Tetris"` · `forgen ferment list/progress/switch/export` — snapshot atómico + log append-only, se rehidrata al reabrir.
- **Skills:** carpetas con `SKILL.md` en `~/.config/forgen/skills/` o `.forgen/skills/` — el catálogo se inyecta al prompt y el agente usa `read_skill`.
- **Seguridad:** keys en keychain, nunca en logs; `permissions: auto` bloquea `sudo`/`rm -rf /`/`chmod 777`; `forgen trace` genera reporte sin secretos; `execution.sandbox: docker` aísla `bash`.

## Contribuir

¿Quieres mejorar forgen? Gracias — así se empieza:

```bash
make build      # compila a bin/forgen
make fmt        # gofmt
make vet        # go vet
make lint       # golangci-lint (si está instalado)
make test       # unit + integración
make test-race  # con -race
```

Convenciones: Go `gofmt`, commits tipo `feat:`, `fix:`, `docs:`, PRs pequeños y con tests. Ver `CONTRIBUTING.md` y `docs/ARCHITECTURE.md` (hexagonal: `core` → `application` → `adapters`).

Si no planeas contribuir código, abrir un issue con `forgen trace` ayuda mucho.

## Tests

```bash
go test ./...                 # todo
go test ./internal/adapters/in/tui -run TestHelpContent -v  # TUI
go test -race ./...           # detector de carreras
```

Cobertura: `go test -cover ./...`

## Licencia

Apache-2.0 — ver [LICENSE](LICENSE).

---

Hecho con Go. Un binario, cero lock-in: cambia de modelo o proveedor editando `config.yaml`.
