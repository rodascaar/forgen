package ferment

import (
	"fmt"
	"strings"

	"github.com/forgen/forgen/internal/core/domain"
)

// ContextBlock renderiza el estado del ferment como instrucciones de system
// prompt para que el agente trabaje hacia el objetivo del proyecto.
func ContextBlock(ferment domain.Ferment) string {
	if ferment.ID == "" {
		return ""
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "## Proyecto activo (Ferment): %s\n", ferment.Name)
	if ferment.Goal != "" {
		fmt.Fprintf(&builder, "Objetivo: %s\n", ferment.Goal)
	}
	if ferment.Criteria != "" {
		fmt.Fprintf(&builder, "Criterios de completitud: %s\n", ferment.Criteria)
	}
	if ferment.Constraints != "" {
		fmt.Fprintf(&builder, "Restricciones: %s\n", ferment.Constraints)
	}
	builder.WriteString(fmt.Sprintf("Estado: %s\n", ferment.Status))

	// Fases y pasos (progreso).
	for phaseIndex, phase := range ferment.Phases {
		fmt.Fprintf(&builder, "\nFase %d: %s [%s]\n", phaseIndex+1, phase.Name, phase.Status)
		for stepIndex, step := range phase.Steps {
			fmt.Fprintf(&builder, "  %d.%d %s [%s]\n", phaseIndex+1, stepIndex+1, step.Task, step.Status)
		}
	}

	// Decisiónes registradas.
	if len(ferment.Decisions) > 0 {
		builder.WriteString("\nDecisiones arquitectónicas:\n")
		for _, decision := range ferment.Decisions {
			fmt.Fprintf(&builder, "  - %s", decision.Description)
			if decision.Rationale != "" {
				fmt.Fprintf(&builder, " (%s)", decision.Rationale)
			}
			builder.WriteString("\n")
		}
	}

	// Memorias (gotchas y convenciones).
	if len(ferment.Memories) > 0 {
		builder.WriteString("\nMemorias del proyecto:\n")
		for _, memory := range ferment.Memories {
			fmt.Fprintf(&builder, "  - [%s] %s\n", memory.Kind, memory.Content)
		}
	}

	return strings.TrimSpace(builder.String())
}

// CurrentStep describe el paso activo, si existe (útil para status).
func CurrentStep(ferment domain.Ferment) string {
	for phaseIndex, phase := range ferment.Phases {
		if phase.Status != domain.PhaseStatusActive {
			continue
		}
		for stepIndex, step := range phase.Steps {
			if step.Status == domain.StepStatusActive {
				return fmt.Sprintf("fase %d (%s) · paso %d: %s", phaseIndex+1, phase.Name, stepIndex+1, step.Task)
			}
		}
	}
	return "sin paso activo"
}
