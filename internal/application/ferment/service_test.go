package ferment_test

import (
	"context"
	"testing"

	"github.com/rodascaar/forgen/internal/adapters/out/storage"
	"github.com/rodascaar/forgen/internal/application/ferment"
	"github.com/rodascaar/forgen/internal/core/domain"
)

func newService(t *testing.T) *ferment.Service {
	t.Helper()
	store, err := storage.NewJSONLFermentStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONLFermentStore: %v", err)
	}
	return ferment.NewService(store)
}

func samplePhases() []domain.Phase {
	return []domain.Phase{
		{Name: "Setup", Steps: []domain.Step{{Task: "init repo"}, {Task: "add ci"}}},
		{Name: "Feature", Steps: []domain.Step{{Task: "implement"}}},
	}
}

func TestFermentLifecycle(t *testing.T) {
	service := newService(t)
	ctx := context.Background()

	created, err := service.Create(ctx, "Build Tetris", "Hacer un Tetris")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Status != domain.FermentStatusDraft {
		t.Fatalf("estado inicial = %q, want draft", created.Status)
	}

	scoped, err := service.Scope(ctx, created.ID, "Hacer un Tetris", "jugable", "sin deps", samplePhases())
	if err != nil {
		t.Fatalf("Scope: %v", err)
	}
	if scoped.Status != domain.FermentStatusPlanned {
		t.Fatalf("estado tras scope = %q, want planned", scoped.Status)
	}
	if len(scoped.Phases) != 2 {
		t.Fatalf("fases = %d, want 2", len(scoped.Phases))
	}

	// Activar primera fase.
	activated, err := service.ActivatePhase(ctx, created.ID, 0)
	if err != nil {
		t.Fatalf("ActivatePhase: %v", err)
	}
	if activated.Status != domain.FermentStatusRunning {
		t.Fatalf("estado = %q, want running", activated.Status)
	}
	if activated.Phases[0].Steps[0].Status != domain.StepStatusActive {
		t.Fatalf("primer paso debería estar activo")
	}

	// Completar pasos hasta acabar el ferment.
	_, _ = service.CompleteStep(ctx, created.ID, 0, 0)
	_, _ = service.CompleteStep(ctx, created.ID, 0, 1) // fase 0 completa
	final, err := service.CompleteStep(ctx, created.ID, 1, 0)
	if err != nil {
		t.Fatalf("CompleteStep: %v", err)
	}
	if final.Status != domain.FermentStatusComplete {
		t.Fatalf("estado final = %q, want complete", final.Status)
	}
	if final.CompletedSteps() != final.TotalSteps() {
		t.Fatalf("progreso = %d/%d, want completo", final.CompletedSteps(), final.TotalSteps())
	}
}

func TestInvalidTransitionFails(t *testing.T) {
	service := newService(t)
	ctx := context.Background()

	created, err := service.Create(ctx, "proyecto", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// draft -> running no es válido (debe pasar por planned).
	if _, err := service.ActivatePhase(ctx, created.ID, 0); err == nil {
		t.Fatal("esperaba error: draft no puede activar fase")
	}

	// draft -> paused no es válido.
	if _, err := service.Pause(ctx, created.ID); err == nil {
		t.Fatal("esperaba error: draft no puede pausar")
	}
}

func TestPauseResume(t *testing.T) {
	service := newService(t)
	ctx := context.Background()

	created, _ := service.Create(ctx, "p", "")
	_, err := service.Scope(ctx, created.ID, "p", "", "", samplePhases())
	if err != nil {
		t.Fatalf("Scope: %v", err)
	}
	_, err = service.ActivatePhase(ctx, created.ID, 0)
	if err != nil {
		t.Fatalf("ActivatePhase: %v", err)
	}
	paused, err := service.Pause(ctx, created.ID)
	if err != nil {
		t.Fatalf("Pause: %v", err)
	}
	if paused.Status != domain.FermentStatusPaused {
		t.Fatalf("estado = %q, want paused", paused.Status)
	}
	resumed, err := service.Resume(ctx, created.ID)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.Status != domain.FermentStatusRunning {
		t.Fatalf("estado = %q, want running", resumed.Status)
	}
}

func TestRecoveryReloadsSnapshot(t *testing.T) {
	store, err := storage.NewJSONLFermentStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONLFermentStore: %v", err)
	}
	service := ferment.NewService(store)
	ctx := context.Background()

	created, _ := service.Create(ctx, "recover", "")
	_, _ = service.Scope(ctx, created.ID, "recover", "", "", samplePhases())
	_, _ = service.ActivatePhase(ctx, created.ID, 0)
	_, _ = service.CompleteStep(ctx, created.ID, 0, 0)

	// Nueva "sesión": un nuevo service sobre el mismo store debe rehidratar
	// el estado exacto (recovery).
	rehydrated, err := service.Load(ctx, created.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if rehydrated.Status != domain.FermentStatusRunning {
		t.Fatalf("estado rehidratado = %q, want running", rehydrated.Status)
	}
	if rehydrated.CompletedSteps() != 1 {
		t.Fatalf("pasos completados = %d, want 1", rehydrated.CompletedSteps())
	}
}

func TestDecisionsAndMemoriesPersist(t *testing.T) {
	service := newService(t)
	ctx := context.Background()

	created, _ := service.Create(ctx, "d", "")
	_, err := service.AddDecision(ctx, created.ID, "usar hexagonal", "testabilidad")
	if err != nil {
		t.Fatalf("AddDecision: %v", err)
	}
	_, err = service.AddMemory(ctx, created.ID, "gotcha", "jsonl es append-only")
	if err != nil {
		t.Fatalf("AddMemory: %v", err)
	}

	loaded, err := service.Load(ctx, created.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded.Decisions) != 1 || len(loaded.Memories) != 1 {
		t.Fatalf("decisiones=%d memorias=%d, want 1 y 1", len(loaded.Decisions), len(loaded.Memories))
	}
}

func TestContextBlockRendersState(t *testing.T) {
	service := newService(t)
	ctx := context.Background()

	created, _ := service.Create(ctx, "tetris", "juego")
	scoped, _ := service.Scope(ctx, created.ID, "juego", "jugable", "", samplePhases())
	activated, _ := service.ActivatePhase(ctx, scoped.ID, 0)

	block := ferment.ContextBlock(activated)
	if block == "" {
		t.Fatal("ContextBlock vacío")
	}
	for _, want := range []string{"tetris", "juego", "Setup", "init repo"} {
		if !contains(block, want) {
			t.Fatalf("ContextBlock no contiene %q:\n%s", want, block)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
