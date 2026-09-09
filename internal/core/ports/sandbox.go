package ports

import (
	"context"

	"github.com/rodascaar/forgen/internal/core/domain"
)

// SandboxBackend impone una SandboxPolicy a nivel de SO para comandos del agente.
// Implementaciones: seatbelt (macOS), bwrap (Linux), docker (contenedor).
// La interfaz existe para que el motor sea reemplazable sin tocar el agente:
// si Apple retira sandbox-exec, se añade un backend nuevo en un solo archivo.
type SandboxBackend interface {
	// Name identifica el backend (seatbelt/bwrap/docker).
	Name() string
	// Available indica si el backend puede operar aquí (binario presente,
	// soporte del kernel, etc). Error explica por qué no.
	Available(ctx context.Context) error
	// Execute corre el comando confinado por la política.
	Execute(ctx context.Context, policy domain.SandboxPolicy, command, workdir string, env []string) (ExecResult, error)
}
