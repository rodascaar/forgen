# Contributing

Gracias por tu interés en contribuir a forgen.

## Principios de ingeniería

El código sigue reglas estrictas de calidad (ver `docs/CHECKLIST.md`):

- **Arquitectura hexagonal**: el dominio y los casos de uso no importan adapters.
- **SOLID / KISS / YAGNI / DRY**: un puerto, una responsabilidad; sin objetos Dios.
- **Fail-fast**: errores nunca silenciosos; `slog` con contexto.
- **Testabilidad**: la lógica de negocio se testea contra fakes; los adapters
  contra `httptest` o pipes en memoria.

## Estructura

```
cmd/forgen/            entry point
internal/core/domain/  entidades puras
internal/core/ports/   interfaces (puertos)
internal/application/  casos de uso
internal/adapters/in/  CLI + TUI + JSON-RPC
internal/adapters/out/ LLM, fs, git, exec, storage, lsp, mcp, ...
internal/app/          composition root (DI manual)
```

## Flujo de trabajo

1. Haz un fork y crea una rama con un nombre descriptivo.
2. Escribe tests para el cambio (`go test ./...`).
3. Asegúrate de que `go vet`, `gofmt` y `golangci-lint` estén limpios:

```bash
make fmt
make vet
make lint
make test
```

4. Abre un PR describiendo el problema y la solución.

## Convenciones de commit

Usamos [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` nueva funcionalidad
- `fix:` corrección de bug
- `refactor:` sin cambio de comportamiento
- `docs:` documentación
- `test:` tests
- `chore:` mantenimiento

## Agregar un proveedor LLM

1. Define un `ProviderType` en `internal/core/domain/config.go`.
2. Implementa `ports.LLMProvider` en `internal/adapters/out/llm/`.
3. Regístralo en `llm.Factory.CreateWithTags`.
4. Añade tests con un servidor SSE fake (`httptest`).

## Agregar una herramienta

1. Define el `ToolDef` con su JSON Schema y ejecución.
2. Regístrala en `tools.Registry` (o vía `registry.Register`).
3. Testea contra un `FileSystem`/`Executor` fake.
