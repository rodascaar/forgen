# forgen

Agente de código que corre en tu terminal. **Agnóstico a lenguaje y proveedor**: funciona con cualquier lenguaje de programación y cualquier proveedor LLM (OpenAI-compatible, Anthropic, Kimchi, local/Ollama).

Escrito en Go con **arquitectura hexagonal**, binario único estático, sin cgo.

## Estado

Fases 1, 2, 3 y 4 implementadas. Ver [`docs/CHECKLIST.md`](docs/CHECKLIST.md) para el plan 0-100%.

## Instalación

```bash
git clone <repo>
cd forgen
go build -o bin/forgen ./cmd/forgen
# opcional: instalar en el PATH
go install ./cmd/forgen
```

## Configuración

```bash
forgen init          # wizard interactivo (proveedores + modelo por defecto)
forgen doctor        # diagnóstico del entorno
```

La configuración vive en `~/.config/forgen/config.yaml` (XDG) y soporta estas capas de precedencia: **defaults < archivo < variables de entorno (`FORGEN_*`) < flags de CLI**.

Ejemplo de `config.yaml`:

```yaml
providers:
  - name: openai
    type: openai_compatible
    base_url: https://api.openai.com/v1
    api_key_env: OPENAI_API_KEY
    models: [gpt-5, gpt-5-mini]
  - name: anthropic
    type: anthropic
    base_url: https://api.anthropic.com
    api_key_env: ANTHROPIC_API_KEY
    models: [claude-sonnet-4-5]
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
```

## Uso

```bash
forgen                              # TUI interactiva (Tab cambia build/plan)
forgen ask "explica este repo"      # petición única headless
forgen ask --json "..."             # salida JSON (eventos para integraciones)
forgen ask --session <id> "..."     # continua una sesión existente
forgen sessions                     # lista sesiones guardadas
forgen sessions resume <id>         # ver una sesión
forgen sessions delete <id>         # eliminar una sesión
forgen config                       # ver configuración efectiva
forgen agent use plan               # cambiar agente por defecto
```

## Proveedores sin lock-in

El núcleo no conoce proveedores. Cada proveedor es una entrada en la config:
- `openai_compatible` — OpenAI, OpenRouter, vLLM, Ollama, Kimi, DeepSeek, etc.
- `anthropic` — protocolo Messages nativo
- `kimchi` — gateway gestionado de Kimchi

## Multi-modelo (orquestación por roles)

Asigna modelos por rol y por fase. Sin config, todo corre en un solo modelo.

```yaml
model_roles:
  explorer: [openai/gpt-5-mini]
  builder: [openai/gpt-5, anthropic/claude-sonnet-4-5]
  reviewer: [anthropic/claude-sonnet-4-5]
model_metadata:
  anthropic/claude-sonnet-4-5: { tier: heavy }
  openai/gpt-5-mini: { tier: light }
```

El orquestador clasifica cada tarea en una fase (explore/plan/build/review/research), elige el modelo por rol y escala a modelos `heavy` para tareas complejas. Cada petición lleva tags de fase/modelo para atribución de costos.

## Proyectos multi-sesión (Ferment)

```bash
forgen ferment new "Build Tetris"   # crea un proyecto
forgen ferment list                  # lista proyectos
forgen ferment progress <id>         # progreso de fases/pasos
forgen ferment switch <id>           # establece el proyecto activo
forgen ferment export <id>           # exporta a JSON
```

El estado persiste con snapshot atómico + log de eventos append-only con hash encadenado: al reabrir `forgen`, el proyecto se rehidrata exactamente donde quedó.

## Skills

Las skills son directorios con un `SKILL.md` (frontmatter `name`/`description`):
- Global: `~/.config/forgen/skills/<nombre>/SKILL.md`
- Proyecto: `.forgen/skills/<nombre>/SKILL.md`

El catálogo se inyecta al system prompt y el agente usa `read_skill` para leer el detalle.

## MCP (Model Context Protocol)

```yaml
mcp_servers:
  filesystem:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "/ruta"]
```

Las herramientas MCP se exponen al agente como `filesystem_<tool>`.

## Herramientas integradas (agnósticas al lenguaje)

`read`, `write`, `edit`, `glob`, `grep`, `bash`, `git_status`, `git_diff`, `web_fetch`, `web_search`. Todas construidas sobre puertos inyectables (testables sin tocar disco real).

## Inteligencia de lenguaje (LSP)

Si el language server del lenguaje está instalado, forgen expone herramientas de código con tipo:

- `lsp_diagnostics` — errores y warnings del lenguaje
- `lsp_hover` — documentación y tipo de un símbolo
- `lsp_definition` / `lsp_references` — navegación
- `lsp_rename` — renombrado seguro en todos los archivos

Soportado: Go (`gopls`), TypeScript/JavaScript (`typescript-language-server`), Rust (`rust-analyzer`), Python (`pyright-langserver`). Las ediciones del agente se sincronizan al server automáticamente.

## Búsqueda web

```yaml
search:
  provider: brave
  api_key_env: BRAVE_API_KEY
```

`web_search` usa la API de Brave; `web_fetch` descarga y extrae el texto de cualquier URL.

## Uso y costos

```bash
forgen usage    # consumo de tokens por modelo
```

Cada llamada registra tokens de entrada/salida con fase y modelo (JSONL append-only).

## UX y plataforma

- **Theming**: paleta configurable en `theme:` (colores de la TUI).
- **Diagnóstico**: `forgen trace` exporta un reporte markdown (sin secretos).
- **Hooks de bash**: scripts ejecutables en `~/.config/forgen/hooks/bash/` (global) y `.forgen/hooks/bash/` (proyecto) que reescriben o bloquean comandos.
- **Sandbox**: `execution.sandbox: docker` ejecuta `bash` en un contenedor aislado.
- **Servidor JSON-RPC**: `forgen serve` expone `agent/run` sobre stdio para integrar con IDEs.
- **Auditoría**: `forgen audit <session-id>`; `forgen sessions export/import` para mover sesiones entre máquinas.

## Permisos

- `auto` — ejecuta herramientas rutinarias; detecta y pregunta por comandos destructivos (`sudo`, `rm -rf /`, `dd`, `chmod 777`, fork bombs).
- `on_request` — pregunta por herramientas sensibles (write/edit/bash).
- `never` — solo lectura.

## Desarrollo

```bash
make build      # compila a bin/forgen
make test       # tests unitarios + integración
make test-race  # con detector de carreras
make vet        # go vet
make fmt        # gofmt
make lint       # golangci-lint
```

## Arquitectura

```
cmd/forgen/             entry point
internal/
  core/domain/          entidades puras (sin dependencias)
  core/ports/           interfaces (puertos del hexágono)
  application/          casos de uso (runner, session, config, tools, permission)
  adapters/in/          CLI (cobra) + TUI (bubbletea)
  adapters/out/         LLM, fs, git, exec, storage, language
  app/                  composition root (DI manual)
```

Los cambios fluyen hacia adentro: los casos de uso dependen solo de puertos; los adapters implementan puertos.

## Licencia

Apache-2.0