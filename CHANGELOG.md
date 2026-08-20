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

## [No liberado]

[0.1.0]: https://github.com/forgen/forgen/releases/tag/v0.1.0
