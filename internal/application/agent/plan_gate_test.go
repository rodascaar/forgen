package agent_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/rodascaar/forgen/internal/adapters/out/fs"
	"github.com/rodascaar/forgen/internal/application/agent"
	appplan "github.com/rodascaar/forgen/internal/application/plan"
	"github.com/rodascaar/forgen/internal/application/session"
	"github.com/rodascaar/forgen/internal/application/tools"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// countingExecutor cuenta ejecuciones reales de bash.
type countingExecutor struct{ calls int }

func (c *countingExecutor) Execute(context.Context, string, string, []string) (ports.ExecResult, error) {
	c.calls++
	return ports.ExecResult{Stdout: "done", ExitCode: 0}, nil
}

// providerEmite bash una vez y luego texto final.
func bashOnceProvider(text string) *fakeProvider {
	n := 0
	return &fakeProvider{streamFn: func(ctx context.Context, request ports.ChatRequest, handler ports.StreamHandler) error {
		n++
		if n == 1 {
			return handler(ports.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Name: "bash", Arguments: map[string]any{"command": "echo hi"}}})
		}
		if err := handler(ports.TextDeltaEvent{Text: text}); err != nil {
			return err
		}
		return handler(ports.DoneEvent{Reason: domain.FinishReasonStop})
	}}
}

func newGateRunner(t *testing.T, provider ports.LLMProvider, execCounter *countingExecutor, store ports.SessionStore) *agent.Runner {
	t.Helper()
	fileSystem := fs.New(t.TempDir())
	registry := tools.NewRegistry(fileSystem, execCounter, nilGit{}, 1000)
	registry.Register(appplan.NewExitPlanTool())
	sessions := session.NewService(store)
	messenger := &recordingMessenger{}
	runner, err := agent.NewRunner(agent.Options{
		Provider:      provider,
		Tools:         registry,
		Decider:       allowAllDecider{},
		Responder:     allowResponder{},
		Messenger:     messenger,
		Sessions:      sessions,
		SystemPrompt:  func(ctx context.Context) (string, error) { return "system", nil },
		MaxIterations: 5,
		Logger:        slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return runner
}

func TestPlanGateBlocksPendingMutations(t *testing.T) {
	execCounter := &countingExecutor{}
	store := newMemorySessionStore()
	runner := newGateRunner(t, bashOnceProvider("fin"), execCounter, store)
	sess := domain.Session{ID: "g1", Workspace: "/tmp", Model: domain.Model{Provider: "fake", ID: "m"}, Agent: "build", PlanStatus: "pending"}
	result, err := runner.Run(context.Background(), agent.RunInput{
		Session: sess, Agent: domain.BuiltinAgents()[0], Workspace: "/tmp", UserPrompt: "hazlo",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if execCounter.calls != 0 {
		t.Fatalf("gate debe bloquear ejecución, calls=%d", execCounter.calls)
	}
	stored, _ := store.Load(context.Background(), "g1")
	found := false
	for _, m := range stored.Messages {
		if strings.Contains(m.Text(), "PLAN GATE") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("el modelo debe ver el mensaje PLAN GATE")
	}
	if result.FinalText != "fin" {
		t.Fatalf("FinalText=%q", result.FinalText)
	}
}

func TestPlanGateAllowsWhenApproved(t *testing.T) {
	execCounter := &countingExecutor{}
	store := newMemorySessionStore()
	runner := newGateRunner(t, bashOnceProvider("fin"), execCounter, store)
	sess := domain.Session{ID: "g2", Workspace: "/tmp", Model: domain.Model{Provider: "fake", ID: "m"}, Agent: "build", PlanStatus: "approved"}
	if _, err := runner.Run(context.Background(), agent.RunInput{
		Session: sess, Agent: domain.BuiltinAgents()[0], Workspace: "/tmp", UserPrompt: "hazlo",
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if execCounter.calls != 1 {
		t.Fatalf("aprobado debe ejecutar, calls=%d", execCounter.calls)
	}
}

func TestPlanGateAllowsWithoutPlan(t *testing.T) {
	execCounter := &countingExecutor{}
	store := newMemorySessionStore()
	runner := newGateRunner(t, bashOnceProvider("fin"), execCounter, store)
	sess := domain.Session{ID: "g3", Workspace: "/tmp", Model: domain.Model{Provider: "fake", ID: "m"}, Agent: "build"}
	if _, err := runner.Run(context.Background(), agent.RunInput{
		Session: sess, Agent: domain.BuiltinAgents()[0], Workspace: "/tmp", UserPrompt: "hazlo",
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if execCounter.calls != 1 {
		t.Fatalf("sin plan debe ejecutar normal, calls=%d", execCounter.calls)
	}
}

func TestPlanArtifactWriteSetsPending(t *testing.T) {
	n := 0
	provider := &fakeProvider{streamFn: func(ctx context.Context, request ports.ChatRequest, handler ports.StreamHandler) error {
		n++
		if n == 1 {
			return handler(ports.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Name: "write", Arguments: map[string]any{"path": ".forgen/plans/plan.md", "content": "# plan"}}})
		}
		if err := handler(ports.TextDeltaEvent{Text: "plan listo"}); err != nil {
			return err
		}
		return handler(ports.DoneEvent{Reason: domain.FinishReasonStop})
	}}
	execCounter := &countingExecutor{}
	store := newMemorySessionStore()
	runner := newGateRunner(t, provider, execCounter, store)
	planAgent := domain.Agent{Name: "plan", IsReadOnly: true}
	sess := domain.Session{ID: "g4", Workspace: "/tmp", Model: domain.Model{Provider: "fake", ID: "m"}, Agent: "plan"}
	if _, err := runner.Run(context.Background(), agent.RunInput{
		Session: sess, Agent: planAgent, Workspace: "/tmp", UserPrompt: "planifica",
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	stored, _ := store.Load(context.Background(), "g4")
	if stored.PlanStatus != "pending" {
		t.Fatalf("escribir plan debe marcar pending, got %q", stored.PlanStatus)
	}
}

func TestExitPlanModeSetsApproved(t *testing.T) {
	n := 0
	provider := &fakeProvider{streamFn: func(ctx context.Context, request ports.ChatRequest, handler ports.StreamHandler) error {
		n++
		if n == 1 {
			return handler(ports.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Name: "exit_plan_mode", Arguments: map[string]any{"plan": "hacer X"}}})
		}
		if err := handler(ports.TextDeltaEvent{Text: "ok"}); err != nil {
			return err
		}
		return handler(ports.DoneEvent{Reason: domain.FinishReasonStop})
	}}
	execCounter := &countingExecutor{}
	store := newMemorySessionStore()
	runner := newGateRunner(t, provider, execCounter, store)
	planAgent := domain.Agent{Name: "plan", IsReadOnly: true}
	sess := domain.Session{ID: "g5", Workspace: "/tmp", Model: domain.Model{Provider: "fake", ID: "m"}, Agent: "plan", PlanStatus: "pending"}
	if _, err := runner.Run(context.Background(), agent.RunInput{
		Session: sess, Agent: planAgent, Workspace: "/tmp", UserPrompt: "aprueba",
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	stored, _ := store.Load(context.Background(), "g5")
	if stored.PlanStatus != "approved" {
		t.Fatalf("exit_plan_mode debe aprobar, got %q", stored.PlanStatus)
	}
}

var _ = appplan.ExitPlanToolName
