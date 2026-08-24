// Package tui implementa la interfaz interactiva del terminal (bubbletea).
package tui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/rodascaar/forgen/internal/core/domain"
)

// Styles agrupa los estilos renderizados desde el tema.
type Styles struct {
	user      lipgloss.Style
	assistant lipgloss.Style
	tool      lipgloss.Style
	toolDone  lipgloss.Style
	notice    lipgloss.Style
	err       lipgloss.Style
	accent    lipgloss.Style
	dim       lipgloss.Style
	// brand es el color de marca forgen (Lima ácida #A6D93B), fijo e
	// independiente del tema del usuario. Se usa para el logotipo.
	brand lipgloss.Style
}

// newStyles construye los estilos desde un tema.
func newStyles(theme domain.Theme) Styles {
	return Styles{
		user:      lipgloss.NewStyle().Foreground(lipgloss.Color(theme.User)).Bold(true),
		assistant: lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Assistant)),
		tool:      lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Tool)),
		toolDone:  lipgloss.NewStyle().Foreground(lipgloss.Color(theme.ToolDone)),
		notice:    lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Notice)),
		err:       lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Error)).Bold(true),
		accent:    lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Accent)).Bold(true),
		dim:       lipgloss.NewStyle().Foreground(lipgloss.Color(theme.Dim)),
		// Hex #A6D93B == TrueColor 38;2;166;217;59 (Lima ácida). Emite el ANSI
		// 24-bit exacto cuando el terminal lo soporta.
		brand: lipgloss.NewStyle().Foreground(lipgloss.Color("#A6D93B")),
	}
}

// forKind devuelve el estilo según el tipo de línea del transcript.
func (s Styles) forKind(kind string) lipgloss.Style {
	switch kind {
	case "user":
		return s.user
	case "assistant":
		return s.assistant
	case "tool":
		return s.tool
	case "tool_done":
		return s.toolDone
	case "notice":
		return s.notice
	case "error":
		return s.err
	default:
		return s.dim
	}
}
