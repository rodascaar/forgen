//go:build !darwin

package sandbox

import (
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// platformBackend es nil fuera de darwin (Seatbelt es macOS-only).
func platformBackend(_ string) ports.SandboxBackend { return nil }

// profileTextForPolicy no disponible fuera de darwin (bwrap no usa perfiles de texto).
func profileTextForPolicy(_ domain.SandboxPolicy) string {
	return "(perfil de texto no disponible en esta plataforma)"
}
