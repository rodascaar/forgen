// Package ferment implementa la gestión de proyectos multi-sesión (Ferment).
package ferment

import (
	"fmt"

	"github.com/forgen/forgen/internal/core/domain"
)

// allowedTransitions define las transiciones válidas del estado del ferment.
var allowedTransitions = map[domain.FermentStatus]map[domain.FermentStatus]bool{
	domain.FermentStatusDraft: {
		domain.FermentStatusPlanned: true,
	},
	domain.FermentStatusPlanned: {
		domain.FermentStatusRunning: true,
	},
	domain.FermentStatusRunning: {
		domain.FermentStatusPaused:   true,
		domain.FermentStatusComplete: true,
	},
	domain.FermentStatusPaused: {
		domain.FermentStatusRunning: true,
	},
}

// transition valida y aplica un cambio de estado.
func transition(current, next domain.FermentStatus) (domain.FermentStatus, error) {
	if current == next {
		return current, nil
	}
	if allowedTransitions[current][next] {
		return next, nil
	}
	return current, fmt.Errorf("transición de estado inválida: %s -> %s", current, next)
}

// StartDraft inicia un ferment en estado draft.
func StartDraft(name, goal string) domain.Ferment {
	return domain.Ferment{
		Name:   name,
		Goal:   goal,
		Status: domain.FermentStatusDraft,
	}
}

// scopePlan fija el alcance (goal, criterios, restricciones) y las fases.
func scopePlan(ferment domain.Ferment, goal, criteria, constraints string, phases []domain.Phase) (domain.Ferment, error) {
	next, err := transition(ferment.Status, domain.FermentStatusPlanned)
	if err != nil {
		return ferment, err
	}
	ferment.Status = next
	ferment.Goal = goal
	ferment.Criteria = criteria
	ferment.Constraints = constraints
	ferment.Phases = phases
	return ferment, nil
}

// activatePhase activa una fase específica.
func activatePhase(ferment domain.Ferment, phaseIndex int) (domain.Ferment, error) {
	if ferment.Status == domain.FermentStatusPlanned || ferment.Status == domain.FermentStatusPaused {
		next, err := transition(ferment.Status, domain.FermentStatusRunning)
		if err != nil {
			return ferment, err
		}
		ferment.Status = next
	}
	if ferment.Status != domain.FermentStatusRunning {
		return ferment, fmt.Errorf("no se puede activar una fase en estado %s", ferment.Status)
	}
	if phaseIndex < 0 || phaseIndex >= len(ferment.Phases) {
		return ferment, fmt.Errorf("índice de fase %d fuera de rango", phaseIndex)
	}
	if ferment.Phases[phaseIndex].Status == domain.PhaseStatusCompleted {
		return ferment, fmt.Errorf("la fase %d ya está completada", phaseIndex)
	}
	for index := range ferment.Phases {
		if index == phaseIndex {
			ferment.Phases[index].Status = domain.PhaseStatusActive
			if len(ferment.Phases[index].Steps) > 0 {
				ferment.Phases[index].Steps[0].Status = domain.StepStatusActive
			}
		} else if ferment.Phases[index].Status == domain.PhaseStatusActive {
			ferment.Phases[index].Status = domain.PhaseStatusPending
		}
	}
	return ferment, nil
}

// completeStep marca un paso como completado y avanza las fases automáticamente.
func completeStep(ferment domain.Ferment, phaseIndex, stepIndex int) (domain.Ferment, error) {
	if phaseIndex < 0 || phaseIndex >= len(ferment.Phases) {
		return ferment, fmt.Errorf("índice de fase %d fuera de rango", phaseIndex)
	}
	phase := ferment.Phases[phaseIndex]
	if stepIndex < 0 || stepIndex >= len(phase.Steps) {
		return ferment, fmt.Errorf("índice de paso %d fuera de rango", stepIndex)
	}
	if phase.Steps[stepIndex].Status == domain.StepStatusCompleted {
		return ferment, fmt.Errorf("el paso %d ya está completado", stepIndex)
	}

	phase.Steps[stepIndex].Status = domain.StepStatusCompleted
	// Avanzar al siguiente paso de la misma fase.
	if stepIndex+1 < len(phase.Steps) {
		phase.Steps[stepIndex+1].Status = domain.StepStatusActive
	} else {
		// Último paso: fase completada.
		phase.Status = domain.PhaseStatusCompleted
		// Activar la siguiente fase pendiente, si existe.
		for next := phaseIndex + 1; next < len(ferment.Phases); next++ {
			if ferment.Phases[next].Status == domain.PhaseStatusPending {
				ferment.Phases[next].Status = domain.PhaseStatusActive
				if len(ferment.Phases[next].Steps) > 0 {
					ferment.Phases[next].Steps[0].Status = domain.StepStatusActive
				}
				break
			}
		}
	}
	ferment.Phases[phaseIndex] = phase

	if ferment.AllPhasesComplete() {
		next, err := transition(ferment.Status, domain.FermentStatusComplete)
		if err != nil {
			return ferment, err
		}
		ferment.Status = next
	}
	return ferment, nil
}

// pause pausa el ferment.
func pause(ferment domain.Ferment) (domain.Ferment, error) {
	next, err := transition(ferment.Status, domain.FermentStatusPaused)
	if err != nil {
		return ferment, err
	}
	ferment.Status = next
	return ferment, nil
}

// resume reanuda el ferment.
func resume(ferment domain.Ferment) (domain.Ferment, error) {
	next, err := transition(ferment.Status, domain.FermentStatusRunning)
	if err != nil {
		return ferment, err
	}
	ferment.Status = next
	return ferment, nil
}
