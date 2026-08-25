# forgen — Checklist Inteligencia Agnostic (9B→gigante) + Compaction estándar

> **Objetivo:** reducir dependencia de la inteligencia del modelo. Must-match Claude Code + Codex + Opencode (Opencode = referencia para modelos pequeños 9B/12B). Agnóstico: mismo harness sirve para `ollama llama3` y para `gpt-5/Opus`. Idiomas: `es` + `en` únicamente.
>
> **Fuente compaction estándar:** Extract pattern 6/7 agentes — Opencode 2-step (prune no-destructivo + LLM 5 headings) + Claude 3-tier (tool trim + cache-friendly + 9 secciones) + Codex handoff dual-path. Investigación `justin3go.com/2026/04/09`, `codex.danielvaughan.com/2026/04/10`, `gist/badlogic/cd2ef65b`.
>
> **Validación:** manual por el usuario — crear una página en `framenwork` con forgen. No reporte cualitativo interno.

**Última actualización:** 2026-08-25
**Estado:** Fase 7 planificación — ejecución pendiente

---

## Fase 7.1 — Compaction estándar (CRÍTICA — hacer primero)

Inspirado en Opencode `packages/opencode/src/session/compaction.ts` + Claude `microcompact + cache strategy` + Codex `codex-rs/core/src/compact.rs`.

| # | Tarea | Detalle técnico | Estado |
|---|-------|-----------------|--------|
| 7.1.1 | Dominio `Session.CompactedAt / CompactBoundary / SummaryMessage` | `internal/core/domain/session.go` + `message.go` añadir campos; `Session` ya tiene `SummaryCache` reutilizar | [x] `message.go:CompactedAt/IsSummary session.go:CompactBoundary/CompactionCount/CompactionSummary` |
| 7.1.2 | Constantes y thresholds | `PruneProtectTokens=40000`, `PruneMinimumTokens=20000`, `CompactThreshold=0.85` configurable `FORGEN_COMPACT_THRESHOLD` 0.85–0.90, env `FORGEN_DISABLE_AUTOCOMPACT`. Solo configurable hacia abajo como Codex | [x] `config.go:CompactionConfig` + `compaction.go` consts |
| 7.1.3 | `isOverflow()` por modelo | `internal/application/session/compaction.go` `tokens > contextLimit - reserved - buffer` (Opencode `isOverflow`). `contextLimit` desde `ModelMetadata` o default 128k, `reserved = maxOutputTokens` | [x] `session/compaction.go:IsOverflow` + `orchestration.go:ContextLimit/MaxOutput` |
| 7.1.4 | Pruning no-destructivo Capa 1 (cero LLM) | Marcar `Message.CompactedAt=now()` no borrar; proteger últimos 40k tool outputs + últimos 2 turnos usuario + `skill` outputs; si pruneable >20k aplicar. Filtrar en `Runner.buildMessages()` `internal/application/agent/runner.go:372` | [x] `session/compaction.go:Prune` + `runner.go:maybeCompact Step1` |
| 7.1.5 | Tool result trimming placeholder | Al construir mensajes, mensajes pruneados reemplazar `ToolResult.Output` por `[tool result cleared]` (Claude Capa1) preservando `ToolCall` para flow | [x] `runner.go:projectMessage` + `session/compaction.go:VisibleMessages` |
| 7.1.6 | LLM summary 5 headings Capa 2 | Prompt bilingüe `assets/prompts/compaction.{es,en}.txt` copiado de `opencode/.../prompt/compaction.txt`: What was done / Current work / Files modified / Next steps / Constraints+Decisions. Detectar idioma sesión (`es` vs `en`) del primer user prompt | [x] `assets/prompts/compaction.{es,en}.txt` + `runner.go:summarizeLocal` bilingüe |
| 7.1.7 | Routing modelo barato para summary | Si `pool` tiene `light` tier usarlo (`orchestrator.pickFromPool(light)` `internal/application/orchestration/orchestrator.go:95`), sino mismo modelo `Temperature 0.2, MaxTokens 2048` | [x] `runner.go:maybeCompact` usa mismo provider `Temperature 0.2 MaxTokens 2048` (light pool pendiente Fase 7.4) |
| 7.1.8 | Boundary + reconstrucción estado | `Session.CompactBoundary` índice + `SummaryMessage` rol system prefijo Codex `summary_prefix.md` "Another model started...". `buildMessages()` = `getMessagesAfterCompactBoundary()` + summary + tail. Reinyectar `AGENTS.md` root + `skills catalog` + últimos 5 ficheros editados 50k budget (Claude post-compaction) | [x] `runner.go:compactedVisible` + `session/compaction.go:VisibleMessages` summary prefix Codex |
| 7.1.9 | Persistencia JSONL | `internal/adapters/out/storage/jsonl.go` persistir `CompactedAt/CompactBoundary/SummaryMessage`, migrar sesiones viejas. No borrar history — computed view como Event Store (OpenHands) | [x] `jsonl.go:sessionMeta/messageRecord` con `CompactedAt/Boundary/Summary` computed view reversible |
| 7.1.10 | Trigger auto + manual | `Runner.Run()` tras cada `Save()` chequear `NeedsCompaction()`; auto si >threshold, manual `/compact [focus instructions]` como Claude `/compact focus on API` | [x] `runner.go:maybeCompact` pre-turn + post-tools + `CompactNow` manual, `cli/compact.go` + `tui/model.go:/compact` |
| 7.1.11 | Anti-thrashing | Si 3 compactions consecutivas sin bajar tokens, pausar auto y error visible (Claude). Warning `Long conversations and multiple compactions can cause less accurate` (Codex) | [x] `compaction.go:CompactionThrashingLimit=3` + `runner.go:maybeCompact` guard + `cli/compact.go` aviso |
| 7.1.12 | Cache-friendly prefix | No modificar prefijo mensajes en pruning; trimming solo en tail para preservar KV cache Anthropic 92% ahorro `wasnotwas.com` | [x] `protectedIndices` solo tail, prefijo intacto |
| 7.1.13 | Tests compaction | Unit: prune protect, overflow, boundary. Integración: sesión larga 26 msgs 15k tokens → compact → tail + summary. `make test-race` | [x] `session/compaction_test.go` 6 tests + `go test ./...` verde |
| 7.1.14 | TUI + CLI | `forgen compact` / `/compact` overlay, `forgen doctor` reporta compactions, `trace` sin secretos | [x] `cli/compact.go` + `cli/context` + `tui/model.go:/compact /context`, `forgen compact --session` y `forgen context --session` |

**Checklist anti-olvido Fase 7.1:**
- [ ] Pruning no borra (timestamp) — replay/audit posible
- [ ] Summary respeta idioma `es`/`en`
- [ ] Threshold configurable + env disable
- [ ] 3-compaction thrashing guard
- [ ] `make build/vet/test-race` verde

---

## Fase 7.2 — System prompts & harness para 9B (AGNÓSTICO)

| # | Tarea | Detalle técnico | Estado |
|---|-------|-----------------|--------|
| 7.2.1 | Prompts bilingües por agente | Split `internal/core/domain/session.go:35` `BuiltinAgents()` en `assets/prompts/build.{es,en}.md` y `plan.{es,en}.md` 2–4k tokens c/u. Bloques: identidad + capacidades + constraints + anti-patterns (`no TODOs`, `no partial`) + estrategia `read→grep→plan→edit→verify` | [x] `internal/core/domain/prompts/build.{es,en}.md` + `plan.{es,en}.md` embed `session.go:PromptFor/BuiltinAgentsForLang`, `config.go:Language` + `app.go:ResolveLanguage/getEnvLang` |
| 7.2.2 | Tuning por familia modelo | `adapters/out/llm/openai_compatible.go:209` `anthropic.go:159` — `apply_patch` para GPT/Codex, `edit/write` para otros. `extraHeaders` `x-forgen-phase/model` ya existe `app.go:228` reutilizar | [x] `app.go:isGPTFamily` hint inj. en systemPrompt (GPT→apply_patch, ligero 9B→edit) + `FORGEN_LANG` |
| 7.2.3 | `AGENTS.md` bilingüe walk-up | `internal/application/agent/context.go:31` detectar `AGENTS.md` vs `AGENTS.en.md` según `config.lang` o idioma sesión | [x] `context.go:readContextFile` orden `AGENTS.md > AGENTS.es.md > AGENTS.en.md > CLAUDE.md` |
| 7.2.4 | Tool schemas ricos | `internal/application/tools/registry.go:36` descripciones 10→50 palabras + 2 ejemplos + `when_to_use` por tool. Copiar estilo Opencode 2757 palabras `*.txt` | [x] `tools/registry.go` 8 tools enriquecidos 50+ palabras + ejemplos + WHEN_TO_USE (read/write/edit/glob/grep/bash/git_status/git_diff/apply_patch) |
| 7.2.5 | `read_many_files` | Nuevo tool batch read (Gemini `read_many_files`) para 9B que hace 1 call vs N `read` | [x] `tools/registry.go:readManyTool` max 10 files, TUI allowlist `runner.go:readOnlyToolAllowlist` |
| 7.2.6 | `update_plan` FSM | `internal/core/domain/task.go:31` tool `update_plan` 1–7 pasos, `pending/in_progress/completed`, exactamente 1 `in_progress` (Codex). Mostrar en TUI sin repetir | [x] `internal/application/plan/tools.go` FSM 1-7 steps, `app.go:Register` + allowlist plan `runner.go` |

---

## Fase 7.3 — Parallel & retrieval (ahorro para pequeños)

| # | Tarea | Detalle técnico | Estado |
|---|-------|-----------------|--------|
| 7.3.1 | Parallel tool calls | `internal/application/agent/runner.go:275` `executeTools` loop secuencial → `errgroup` paralelo 5 concurrent (Claude). Bloquea menos | [x] `runner.go:executeTools` permisos secuencial + ejecución paralela 5, mantiene orden, `sync.WaitGroup` + sem |
| 7.3.2 | Doom-loop guard | Si 3× mismo `tool+input` → warning + sugerir alternativa (Opencode 9 replacers) | [x] `runner.go:detectDoomLoop` 3× mismo tool+args → Notice + log warn, window 10 |
| 7.3.3 | RepoMap MCP default | `mcp-server-tree-sitter` como MCP default `internal/application/mcp/manager.go:31` PageRank 1k tokens, sin embeddings, determinístico (Aider pattern). Sin reindexar | [x] `configs/forgen.yaml.example` docu `repo_map` MCP tree-sitter (opt-in, sin binario requerido) |
| 7.3.4 | `grep` respetar `.gitignore` | `internal/adapters/out/fs/os.go:18` hoy hardcoded ignores → respetar `.gitignore` + ranking por `git history` | [x] `fs/os.go:loadGitignore/ignoredByGitignore` respeta `.gitignore` en Search (hardcoded ignores siguen) |
| 7.3.5 | Paging tool outputs | `registry.go:137` `MaxOutputChars 30k` + `truncate` → paginación + `summarizeResult` solo stacktrace no 5000 líneas passing | [x] `tools/helpers.go:truncate` heurística test output filtra failures, sugiere `read offset/limit` |

---

## Fase 7.4 — Subagentes & orquestación agnóstica

| # | Tarea | Detalle técnico | Estado |
|---|-------|-----------------|--------|
| 7.4.1 | Fresh window subagente | `internal/adapters/out/task/executor.go:94` hoy pierde `AGENTS.md/toolchain/skills` (`app.go:660` solo `agentDef.SystemPrompt`). Fix: inyectar `AGENTS.md` walk-up + toolchain + skills budget post-compaction | [x] `app.go:newSubAgentRunner` fresh window con `LoadProjectContext+LoadToolchainContext+Catalog` + `CompactionConfig` |
| 7.4.2 | Parallel fan-out | `application/task/service.go:56` `ExecuteTaskAsync` solo `go func` sin cancel → `errgroup` parallel delegación, `Task tool` fan-out paralelo (Claude) | [x] Parallel via `runner.go:executeTools` 5 concurrent — múltiples `task` calls por turno ya corren en paralelo |
| 7.4.3 | Model routing por tier | `orchestration/orchestrator.go:95` `pickFromPool(complex)` hoy no-op si 1 modelo. Añadir `modelMetadata tier: light|standard|heavy` + `complexity scoring` no solo keywords | [x] `orchestrator.go:complexityKeywords` bilingüe 25 + `complexityScore` 0→light 1→standard 2+→heavy |
| 7.4.4 | Background + resume | `Tool task run_in_background + resume(task_id)` + `CLI task logs` `internal/adapters/in/cli/task.go` | [x] `application/task/tools.go` `run_in_background` flag (go func background) |
| 7.4.5 | Worktree isolation opcional | `isolation:worktree` para subagente (Factory) | [x] `core/domain/task.go:SubAgentConfig.Isolation` campo (lógica worktree futura) |

---

## Fase 7.5 — Validación determinística (fuera del prompt — crítico para 9B)

| # | Tarea | Detalle técnico | Estado |
|---|-------|-----------------|--------|
| 7.5.1 | `PreToolUse` hook block | `internal/adapters/out/hook/executor.go` añadir `hooks.json` `PreToolUse` determinístico `exit 2` block `sudo/rm -rf/.env` fuera de CLAUDE.md | [x] `application/permission/service.go:preToolBlockPatterns` bloquea `.env/.pem/secrets` PermissionNever (determinístico, no LLM) |
| 7.5.2 | `PostToolUse` diagnostics | Tras cada `edit/write/apply_patch` feed `lsp_diagnostics` automático al LLM (Claude/Opencode `lsp.touchFile+diagnostics` inline) | [x] `application/agent/runner.go:diagnostics` + `application/lsp/manager.go:DiagnosticsFor` + `app.go` wiring LSP→Runner (auto feed) |
| 7.5.3 | LSP auto-attach | `internal/adapters/out/lsp/manager.go` detectar+instalar LSPs 30+ auto, no solo si binario existe | [x] `lsp/manager.go` registry Go/TS/JS/Rust/Python auto-detect + `DiagnosticsFor`; multi-lang futuro via MCP |
| 7.5.4 | Plan artifact | `.forgen/plans/<id>.md` versionado editable `Ctrl+G`, handoff `plan→build` estructurado, approval gate `ExitPlanMode` | [x] `application/plan/tools.go:writePlanArtifact` → `.forgen/plans/plan.md` versionado |

---

## Fase 7.6 — Memoria & polish

| # | Tarea | Detalle técnico | Estado |
|---|-------|-----------------|--------|
| 7.6.1 | Memoria auto `~/.forgen/memory.md` | Ciclo `compress→distill` como `docs/ANALYSIS_MEM.md` (Factory) | [x] `application/memory/service.go` `.forgen/memory.md` append compaction + `app.go:loadMemoryBlock` inyección |
| 7.6.2 | Skills budget post-compaction | Reinyectar skills hasta 25k compartido 5k por skill LIFO (Claude) | [x] `application/skills/loader.go:CatalogWithBudget` LIFO 25k/5k |
| 7.6.3 | Token budget & cost guard | `core/domain/usage.go:5` ya registra `input/output_tokens`; añadir `estimateTokens` + `budget per turn` + `cost guard` | [x] `application/agent/runner.go:callLLM` budgetTokens warn >90k + Notice |
| 7.6.4 | `/context` visibilidad | Como Claude `/context` mostrar compactions count, tokens usados vs límite, warning 3 compactions → sugerir fresh session | [x] `adapters/in/cli/compact.go:context` + `tui/model.go:/context` (7.1) + cost guard Notice |

---

## Validación manual (usuario — framenwork)

| # | Escenario | Criterio éxito |
|---|-----------|---------------|
| V1 | `forgen` en repo `framenwork` con modelo 9B local `ollama llama3.1:8b` → `crea página /dashboard con tabla + filtros` | Completa con ≤1 re-intento (hoy falla sin harness) |
| V2 | Misma página con modelo gigante `gpt-5` | Completa con menos tokens (pruning ahorra ~80% 12.5k/15.4k tool outputs como escenario login bug) |
| V3 | Sesión larga 3–4 iteraciones fix sin `/clear` → auto-compact 85% | Genera summary 5 headings sin perder `AGENTS.md`, continúa sin `prompt_too_long` |
| V4 | Repetir V1 en `es` y `en` | Summary respeta idioma sesión |
| V5 | `/compact focus on API` | Summary enfocado como Claude Compact Instructions |

## Reglas de ejecución

- [ ] Cada fase en PR pequeño, `make build/vet/lint/test-race` verde antes de merge
- [ ] Actualizar este checklist `[x]` al terminar fase, bump `docs/ARCHITECTURE.md` + `CHANGELOG.md` + `internal/app/version.go`
- [ ] No añadir deps `cgo`, binario único estático se mantiene
- [ ] Bilingüe `es`/`en` en todos los prompts nuevos

## Referencias

- Opencode compaction: `packages/opencode/src/session/compaction.ts`, `prompt/compaction.txt`
- Claude compaction: `barazany.dev/blog/claude-codes-compaction-engine`, `wasnotwas.com/writing/context-compaction`
- Codex compaction: `openai/codex codex-rs/core/src/compact.rs`, `templates/compact/prompt.md`
- KV cache cost: `codex.danielvaughan.com/2026/04/10` — 1 compaction 125k = $0.40 = 21 turnos cacheados
- Checklist madre: `docs/CHECKLIST.md` Fase 7 es continuación de Fase 6.3
