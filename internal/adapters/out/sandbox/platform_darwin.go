//go:build darwin

package sandbox

import (
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// platformBackend aporta el backend macOS (Seatbelt) solo en darwin.
func platformBackend(goos string) ports.SandboxBackend {
	if goos == "darwin" {
		return SeatbeltBackend{}
	}
	return nil
}

// profileTextForPolicy expone el perfil SBPL efectivo (debug).
func profileTextForPolicy(policy domain.SandboxPolicy) string {
	return BuildSeatbeltProfile(policy)
}
