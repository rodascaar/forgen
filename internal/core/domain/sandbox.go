package domain

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SandboxMode acota lo que el SO permite hacer a los comandos del agente,
// independiente de los approvals (modelo de dos ejes estilo Codex:
// qué pregunta forgen vs qué permite el kernel).
type SandboxMode string

const (
	// SandboxReadOnly inspecciona sin modificar ni ejecutar con efectos.
	SandboxReadOnly SandboxMode = "read-only"
	// SandboxWorkspaceWrite lee todo y escribe/ejecuta solo dentro del workspace.
	// Es el modo default de baja fricción.
	SandboxWorkspaceWrite SandboxMode = "workspace-write"
	// SandboxFullAccess sin restricciones (peligroso: solo con aprobación explícita).
	SandboxFullAccess SandboxMode = "danger-full-access"
)

// ParseSandboxMode normaliza el modo desde config/flags.
func ParseSandboxMode(raw string) SandboxMode {
	switch SandboxMode(strings.ToLower(strings.TrimSpace(raw))) {
	case SandboxReadOnly:
		return SandboxReadOnly
	case SandboxFullAccess:
		return SandboxFullAccess
	case SandboxWorkspaceWrite, "":
		return SandboxWorkspaceWrite
	default:
		return SandboxWorkspaceWrite
	}
}

// SandboxPolicy es la política efectiva que se impone al SO y se anuncia al modelo.
type SandboxPolicy struct {
	Mode SandboxMode
	// Workspace es la única raíz escribible en workspace-write.
	Workspace string
	// AllowNetwork da acceso a red (binario como Seatbelt: on/off, sin filtro
	// por dominios en v1; el proxy con allowlist es fase posterior).
	AllowNetwork bool
	// ReadOnlyPaths siempre read-only aunque estén bajo el workspace (.git, etc).
	ReadOnlyPaths []string
	// ExtraWritable raíces extra escribibles (caches de toolchain fuera del ws).
	ExtraWritable []string
	// Enforced indica si un backend real la impone (false = degradado a approvals).
	Enforced bool
	// Backend es el nombre del backend que la impone (seatbelt/bwrap/docker/none).
	Backend string
}

// DefaultSandboxPolicy construye la política workspace-write para un workspace.
func DefaultSandboxPolicy(workspace string) SandboxPolicy {
	ws := filepath.Clean(workspace)
	return SandboxPolicy{
		Mode:          SandboxWorkspaceWrite,
		Workspace:     ws,
		AllowNetwork:  false,
		ReadOnlyPaths: []string{filepath.Join(ws, ".git")},
	}
}

// PromptBlock describe la política al modelo para que la conozca antes de
// intentarlo (patrón Codex: la política viaja en el system prompt).
func (p SandboxPolicy) PromptBlock() string {
	var sb strings.Builder
	sb.WriteString("Filesystem sandbox (impuesta por el SO, no negociable):\n")
	switch p.Mode {
	case SandboxReadOnly:
		sb.WriteString("- mode=read-only: puedes leer archivos pero NO escribir ni modificar nada.\n")
	case SandboxFullAccess:
		sb.WriteString("- mode=danger-full-access: sin jaula — actúa con máxima cautela y pide confirmación.\n")
	default:
		fmt.Fprintf(&sb, "- mode=workspace-write: solo puedes escribir dentro de %s.\n", p.Workspace)
	}
	for _, ro := range p.ReadOnlyPaths {
		fmt.Fprintf(&sb, "- read-only siempre (aunque esté bajo el workspace): %s.\n", ro)
	}
	if p.AllowNetwork {
		sb.WriteString("- network=allow: tienes acceso a red.\n")
	} else {
		sb.WriteString("- network=deny: SIN acceso a red (curl/wget/npm install fallarán; pide al usuario si la necesitas).\n")
	}
	if !p.Enforced {
		sb.WriteString("- NOTA: el sandbox del SO no está disponible en esta plataforma; rigen los approvals.\n")
	}
	return strings.TrimSpace(sb.String())
}
