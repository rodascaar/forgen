package ferment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// eventTypes son los tipos de evento del log de auditoría.
const (
	eventCreated      = "ferment.created"
	eventScoped       = "ferment.scoped"
	eventPhaseActive  = "phase.activated"
	eventStepComplete = "step.completed"
	eventPaused       = "ferment.paused"
	eventResumed      = "ferment.resumed"
	eventDecision     = "decision.added"
	eventMemory       = "memory.added"
)

// Service implementa los casos de uso de Ferment con persistencia
// snapshot + evento y hash encadenado para auditabilidad.
type Service struct {
	store ports.FermentStore
	now   func() time.Time
}

// NewService construye el servicio de ferment.
func NewService(store ports.FermentStore) *Service {
	return &Service{store: store, now: time.Now}
}

// Create inicia un ferment en estado draft.
func (s *Service) Create(ctx context.Context, name, goal string) (domain.Ferment, error) {
	ferment := domain.Ferment{
		ID:        uuid.NewString(),
		Name:      name,
		Goal:      goal,
		Status:    domain.FermentStatusDraft,
		CreatedAt: s.now(),
		UpdatedAt: s.now(),
	}
	if err := s.commit(ctx, &ferment, eventCreated, map[string]any{"name": name, "goal": goal}); err != nil {
		return domain.Ferment{}, err
	}
	return ferment, nil
}

// Scope define el alcance y el plan de fases (draft -> planned).
func (s *Service) Scope(ctx context.Context, id, goal, criteria, constraints string, phases []domain.Phase) (domain.Ferment, error) {
	ferment, err := s.store.LoadSnapshot(ctx, id)
	if err != nil {
		return domain.Ferment{}, err
	}
	ferment, err = scopePlan(ferment, goal, criteria, constraints, phases)
	if err != nil {
		return domain.Ferment{}, err
	}
	if err := s.commit(ctx, &ferment, eventScoped, map[string]any{"phases": len(phases)}); err != nil {
		return domain.Ferment{}, err
	}
	return ferment, nil
}

// ActivatePhase activa una fase por índice.
func (s *Service) ActivatePhase(ctx context.Context, id string, phaseIndex int) (domain.Ferment, error) {
	ferment, err := s.store.LoadSnapshot(ctx, id)
	if err != nil {
		return domain.Ferment{}, err
	}
	ferment, err = activatePhase(ferment, phaseIndex)
	if err != nil {
		return domain.Ferment{}, err
	}
	if err := s.commit(ctx, &ferment, eventPhaseActive, map[string]any{"phase": phaseIndex}); err != nil {
		return domain.Ferment{}, err
	}
	return ferment, nil
}

// CompleteStep marca un paso como completado.
func (s *Service) CompleteStep(ctx context.Context, id string, phaseIndex, stepIndex int) (domain.Ferment, error) {
	ferment, err := s.store.LoadSnapshot(ctx, id)
	if err != nil {
		return domain.Ferment{}, err
	}
	ferment, err = completeStep(ferment, phaseIndex, stepIndex)
	if err != nil {
		return domain.Ferment{}, err
	}
	if err := s.commit(ctx, &ferment, eventStepComplete, map[string]any{"phase": phaseIndex, "step": stepIndex}); err != nil {
		return domain.Ferment{}, err
	}
	return ferment, nil
}

// Pause pausa el ferment activo.
func (s *Service) Pause(ctx context.Context, id string) (domain.Ferment, error) {
	ferment, err := s.store.LoadSnapshot(ctx, id)
	if err != nil {
		return domain.Ferment{}, err
	}
	ferment, err = pause(ferment)
	if err != nil {
		return domain.Ferment{}, err
	}
	if err := s.commit(ctx, &ferment, eventPaused, nil); err != nil {
		return domain.Ferment{}, err
	}
	return ferment, nil
}

// Resume reanuda el ferment pausado.
func (s *Service) Resume(ctx context.Context, id string) (domain.Ferment, error) {
	ferment, err := s.store.LoadSnapshot(ctx, id)
	if err != nil {
		return domain.Ferment{}, err
	}
	ferment, err = resume(ferment)
	if err != nil {
		return domain.Ferment{}, err
	}
	if err := s.commit(ctx, &ferment, eventResumed, nil); err != nil {
		return domain.Ferment{}, err
	}
	return ferment, nil
}

// AddDecision registra una decisión arquitectónica.
func (s *Service) AddDecision(ctx context.Context, id, description, rationale string) (domain.Ferment, error) {
	ferment, err := s.store.LoadSnapshot(ctx, id)
	if err != nil {
		return domain.Ferment{}, err
	}
	ferment.Decisions = append(ferment.Decisions, domain.FermentDecision{
		Description: description,
		Rationale:   rationale,
		At:          s.now(),
	})
	if err := s.commit(ctx, &ferment, eventDecision, map[string]any{"description": description}); err != nil {
		return domain.Ferment{}, err
	}
	return ferment, nil
}

// AddMemory registra un gotcha o convención encontrada.
func (s *Service) AddMemory(ctx context.Context, id, kind, content string) (domain.Ferment, error) {
	ferment, err := s.store.LoadSnapshot(ctx, id)
	if err != nil {
		return domain.Ferment{}, err
	}
	ferment.Memories = append(ferment.Memories, domain.Memory{
		Kind:    kind,
		Content: content,
		At:      s.now(),
	})
	if err := s.commit(ctx, &ferment, eventMemory, map[string]any{"kind": kind}); err != nil {
		return domain.Ferment{}, err
	}
	return ferment, nil
}

// Load recupera un ferment por ID.
func (s *Service) Load(ctx context.Context, id string) (domain.Ferment, error) {
	return s.store.LoadSnapshot(ctx, id)
}

// List devuelve los ferments disponibles.
func (s *Service) List(ctx context.Context) ([]domain.Ferment, error) {
	return s.store.List(ctx)
}

// Delete elimina un ferment.
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.store.Delete(ctx, id)
}

// commit persiste el snapshot, registra el evento con hash encadenado.
func (s *Service) commit(ctx context.Context, ferment *domain.Ferment, eventType string, data map[string]any) error {
	ferment.UpdatedAt = s.now()
	if err := s.store.SaveSnapshot(ctx, *ferment); err != nil {
		return fmt.Errorf("guardar snapshot: %w", err)
	}

	event := ports.FermentEvent{
		Type:      eventType,
		FermentID: ferment.ID,
		Data:      data,
	}
	if err := s.store.AppendEvent(ctx, event); err != nil {
		return fmt.Errorf("registrar evento: %w", err)
	}
	return nil
}

// HashEvent calcula el hash de un evento para el encadenamiento.
func HashEvent(event ports.FermentEvent) string {
	payload, _ := json.Marshal(event)
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
