# forgen — Feature Roadmap & Checklist

> Fuente de verdad para no olvidar funcionalidades. Actualizado tras cada fase.

## Estado: v0.1.3 + Fases 1.1-1.3-2.1-2.2-4.1 en main

### FASE 0 — Base Sólida ✅
- [x] Fix crash nil-pointer (tea.NewProgram &model)
- [x] Fix delegación mensajes terminales (wizard/picker)
- [x] Fix reset confirming
- [x] ProviderUsable Ollama local
- [x] Timeout 15s validación
- [x] Tests regresión + release v0.1.4

### FASE 1.1 — todowrite (Planificación visible) ✅
- [x] Domain Todo/TodoList + id.go
- [x] Ports TodoStore
- [x] Storage JSONLTodoStore
- [x] Service TodoService
- [x] CLI forgen todo (add/list/done/undo/remove/move/progress)
- [x] LLM tool todowrite (validación single in_progress)
- [x] TUI overlay /todo + status bar 📋
- [ ] Tests específicos todowrite (pendiente reforzar)
- [ ] Docs README

### FASE 1.2 — task/subagent (Delegación) ✅
- [x] Domain Task/SubAgent (5 tipos: explore/plan/build/review/research)
- [x] Ports TaskStore + TaskExecutor
- [x] Storage JSONLTaskStore + Executor (LLM streaming)
- [x] Service TaskService
- [x] CLI forgen task (create/list/status/cancel)
- [x] LLM tool task
- [x] Registro en App (TodoStore/TaskStore/TaskExecutor)
- [x] TUI picker /task

### FASE 2.1 — patch/apply_patch (Diffs estructurados) ✅
- [x] Tool apply_patch (unified diff + Codex Begin Patch)
- [x] Registro en ToolRegistry
- [ ] Tests patch (pendiente)

### FASE 1.3 — Plan Mode interactivo ✅
- [x] Overlay /todo básico
- [x] Edición interactiva en TUI (↑/↓, Enter/x toggle, d borrar, q cerrar)
- [x] Status bar 📋 d/t
- [x] Slash /todo, /plan, /task
- [x] Tecla P (ver plan)
- [ ] Persistencia .forgen/plan.yaml por proyecto (usa store global, suficiente MVP)

### FASE 2.2 — LSP completo ✅
- [x] lsp_implementation, lsp_type_definition, lsp_document_symbols, lsp_workspace_symbols, lsp_code_action, lsp_completion

### FASE 3 — UX Avanzada
- [ ] web_search (ya existe web_fetch)
- [ ] Remote sessions sync
- [ ] Hooks runners
- [ ] Vision

### FASE 4 — Ecosystem
- [x] Slash /diff, /commit, /review, /test, /lint, /fix, /pr
- [ ] Notebook support
- [ ] VS Code ext, MCP Registry, Plugin System
- [ ] CI/CD SBOM/Cosign

## Próximo recomendado
FASE 1.3 completo → FASE 2.2 LSP → FASE 3 web_search
