package domain

import (
	"slices"
	"time"
)

// FermentStatus es el estado de vida de un proyecto Ferment.
type FermentStatus string

const (
	FermentStatusDraft    FermentStatus = "draft"
	FermentStatusPlanned  FermentStatus = "planned"
	FermentStatusRunning  FermentStatus = "running"
	FermentStatusPaused   FermentStatus = "paused"
	FermentStatusComplete FermentStatus = "complete"
)

// PhaseStatus es el estado de una fase dentro de un Ferment.
type PhaseStatus string

const (
	PhaseStatusPending   PhaseStatus = "pending"
	PhaseStatusActive    PhaseStatus = "active"
	PhaseStatusCompleted PhaseStatus = "completed"
)

// StepStatus es el estado de un paso dentro de una fase.
type StepStatus string

const (
	StepStatusPending   StepStatus = "pending"
	StepStatusActive    StepStatus = "active"
	StepStatusCompleted StepStatus = "completed"
)

// Ferment es un proyecto multi-sesión con plan, fases y pasos.
type Ferment struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Goal        string            `json:"goal"`
	Criteria    string            `json:"criteria"`
	Constraints string            `json:"constraints"`
	Status      FermentStatus     `json:"status"`
	Phases      []Phase           `json:"phases"`
	Decisions   []FermentDecision `json:"decisions"`
	Memories    []Memory          `json:"memories"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// Phase es un hito dentro del proyecto.
type Phase struct {
	ID     string      `json:"id"`
	Name   string      `json:"name"`
	Status PhaseStatus `json:"status"`
	Steps  []Step      `json:"steps"`
}

// Step es una tarea ejecutable dentro de una fase.
type Step struct {
	ID     string     `json:"id"`
	Task   string     `json:"task"`
	Status StepStatus `json:"status"`
}

// FermentDecision registra una elección arquitectónica.
type FermentDecision struct {
	Description string    `json:"description"`
	Rationale   string    `json:"rationale"`
	At          time.Time `json:"at"`
}

// Memory registra un gotcha, convención o patrón encontrado.
type Memory struct {
	Kind    string    `json:"kind"` // gotcha | convention | pattern
	Content string    `json:"content"`
	At      time.Time `json:"at"`
}

// CurrentPhaseIndex devuelve el índice de la fase activa, o -1.
func (f Ferment) CurrentPhaseIndex() int {
	idx := slices.IndexFunc(f.Phases, func(p Phase) bool { return p.Status == PhaseStatusActive })
	if idx >= 0 {
		return idx
	}
	return -1
}

// CompletedSteps cuenta los pasos completados en todo el ferment.
func (f Ferment) CompletedSteps() int {
	total := 0
	for _, phase := range f.Phases {
		for _, step := range phase.Steps {
			if step.Status == StepStatusCompleted {
				total++
			}
		}
	}
	return total
}

// TotalSteps cuenta todos los pasos del ferment.
func (f Ferment) TotalSteps() int {
	total := 0
	for _, phase := range f.Phases {
		total += len(phase.Steps)
	}
	return total
}

// AllPhasesComplete indica si todas las fases están completadas.
func (f Ferment) AllPhasesComplete() bool {
	return slices.ContainsFunc(f.Phases, func(p Phase) bool { return p.Status != PhaseStatusCompleted }) == false
}
