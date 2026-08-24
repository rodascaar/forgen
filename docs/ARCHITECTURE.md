# forgen — Arquitectura

> Harness → Agente → Tool/API + LLM Proxy → Herramienta / MODELOS. Hexagonal, agnóstico a proveedor y lenguaje, binario único Go.

## Diagrama (tu idea)

```
                       Usuario
                          │
                          ▼
                     ┌─────────┐
                     │ Harness │  CLI cobra + TUI bubbletea + JSON-RPC (acp) + Runner loop + Session + Permisos
                     └────┬────┘
                          │
                          ▼
                     ┌─────────┐
                     │ Agente  │  domain.Agent (build/plan) + system prompt + orchestration (fase+ tier)
                     └────┬────┘
                          │
                 ┌────────┴────────┐
                 ▼                 ▼
              Tool/API          LLM Proxy
                 │                 │
                 ▼                 ▼
          Herramienta        MODELOS Proveedores
   read/write/edit/glob/   openai_compatible (OpenAI/OpenRouter/Ollama/vLLM/Kimi/DeepSeek/Groq/Together)
   grep/bash/git/lsp/mcp/  anthropic (Messages) · kimchi (gateway) · local
   web_fetch/search/todo/task
         │
         └─► FileSystem / Executor / Git / MCPClient / SearchProvider (puertos inyectables)
```

## Capas hexagonales

```
cmd/forgen/            entry point (cobra root, interactive TUI)
internal/
  core/domain/         entidades puras: Model, Agent, Session, Message, ToolCall, PermissionRule, Ferment, Task/SubAgentConfig
  core/ports/          interfaces: LLMProvider, ToolExecutor, FileSystem, Executor, SessionStore, MCPClient, PermissionDecider/Responder, Messenger
  application/         casos de uso: agent.Runner, session, config, permission, orchestration, ferment, mcp, skills, lsp, web, todo, task, usage
  adapters/in/         CLI (cobra) + TUI (bubbletea) + JSON-RPC (acp) — adaptadores de entrada
  adapters/out/        LLM (openai_compatible/anthropic/kimchi + client retry), fs, exec, hook, sandbox, git, storage(JSONL/YAML), search(brave), mcp(stdio/http/sse), lsp, credentials
  app/                 composition root (DI manual, sin wire): App.NewApp cablea todo, ResolveRunModel, NewRunner
```

**Regla hexagonal:** `core` y `application` no importan `adapters`; dependencias apuntan hacia adentro. Ports en `core/ports` son el contrato.

## Flujo de un turno (Runner)

`internal/application/agent/runner.go:114 Run`:

1. `SessionService.AppendMessage(userPrompt)` — persiste.
2. `systemPrompt()` — compone `agent.SystemPrompt + LoadProjectContext(AGENTS.md/CLAUDE.md walk-up) + LoadToolchainContext + ferment block + skills catalog` (`internal/application/agent/context.go`).
3. `visibleTools(agent)` — filtra por `AllowedTools/DeniedTools`; `plan` oculta `write/edit/bash` (`runner.go:309`).
4. Loop `maxIterations=50`: `provider.StreamChat(ChatRequest{Model,Messages,Tools})` → `TextDelta/ToolCall/Usage/Done` → `executeTools()` → `Decide()` (reglas exactas + dangerousPatterns `sudo/rm -rf/chmod 777` → `on_request` incluso en auto) → `Responder.Confirm()` si `on_request` → `ToolExecutor.Execute` → append `ToolResult` → `Save()` → repetir hasta `Done` sin tools.
5. `recordUsage()` → `UsageRecorder` JSONL.

## Harness

`internal/app/app.go:44 App` construye todo con `ResolvePaths()` XDG (`~/.config/forgen`, `~/.local/share/forgen`). `NewRunner()` (`app.go:228`) inyecta `permission.NewService(mode, workspace, configRules, persistedRules)` (`internal/application/permission/service.go`) y `systemPrompt` closure. `Paths.RulesFile` (`~/.local/share/forgen/permissions.yaml`) vía `JSONPermissionStore` (0600).

TUI: `bubbletea` con streaming, spinner, `Tab` build↔plan, `?` help, `PgUp/PgDn`. Headless: `forgen ask "prompt" [--json] [--session id]`.

## LLM Proxy

`ports.LLMProvider` (`internal/core/ports/llm_provider.go:53`) con `StreamChat` + `ListModels`. `adapters/out/llm/factory.go:39 CreateWithKeyResolver` elige `openai_compatible/anthropic/kimchi` según `ProviderConfig.Type`; `Client` (`adapters/out/llm/client.go`) añade `ExtraHeaders` para `phase/model` tags (Kimchi-style) y retry/backoff. Credenciales vía `CredentialStore` (keychain) + env fallback (`app.go:408 providerAPIKey`), nunca en logs.

## Tool/API

`ports.ToolExecutor` (`internal/core/ports/messenger.go:10`) + `tools.Registry` (`internal/application/tools/registry.go:26`) con `ObjectSchema` JSON Schema genérico. Tools nativas: `read/write/edit/glob/grep/bash/git_status/git_diff/apply_patch` contra `FileSystem/Executor/Git` testeables. `hook.NewExecutor` rewrites bash vía `~/.config/forgen/hooks/bash` (RTK pattern) y `sandbox.DockerExecutor` opt-in.

MCP: `ports.MCPClient` (`ListTools/CallTool/Close`). `adapters/out/mcp/stdio.go` (subproceso JSON-RPC) + `adapters/out/mcp/http.go` (HTTP/SSE). `application/mcp.Manager.Start()` (`manager.go:31`) registra `<server>_<tool>` en `Registry` y soporta `type: stdio|http|sse` (`domain.MCPServerConfig`). Migración desde Claude Code/OpenCode/Cursor (`application/mcp/migrate.go`).

Permisos: wildcard `mcp_*` en `matchRule()` (`permission/service.go:88`), persistencia `permissions.yaml`, CLI `forgen permissions remember <allow|deny> tool args-json` + `revoke <id>` (stable sha256).

## Subagentes

`domain.Task` + `SubAgentConfig{Type,Name,Description,Tools,Prompt,MaxTurns,Timeout}` + `SubAgentRegistry` (`internal/core/domain/task.go:88`). `adapters/out/task/executor.go` con `ProviderResolver` (orquestación por tier) y `RunnerFactory` inyectado por `App` (`app.go:179`): crea `subAgentRunner` aislado con `registry.LookupTools(cfg.Tools)` y `permission mode:auto` (no interactivo) + `noopMessenger`. Carga overrides desde `.forgen/agents/<type>.md` frontmatter (`task.go:100 LoadSubAgentRegistry`).

## Ferment (proyectos multi-sesión)

FSM `draft→planned→running→paused→complete`, `Ferment/Phase/Step/Decision/Memory`, event store append-only con hashes, recovery exacto (`internal/application/ferment`, `adapters/out/storage/ferment.go`).

## Referencias cruzadas con los 6 repos ejemplo

- Kimchi: orquestación por roles + phase tags + Ferment + migración MCP.
- Claude Code / Codex: plugins/subagents como markdown + sandbox/policies + ACP.
- Fx: minimalismo Zig, `permissions remember` con stable IDs + narrow safety review + WASM embed.
- Gemini CLI: MCP extensible + GEMINI.md walk-up + checkpoint.
- Opencode: build/plan agents (Tab) + @general subagent + todo/task tools.

## Decisiones no negociables

Hexagonal, SOLID (un puerto una responsabilidad), KISS/YAGNI, DRY, nombres explícitos, fail-fast con `slog` context, testabilidad (fakes + httptest), seguridad (keys 0600, nunca en logs).
