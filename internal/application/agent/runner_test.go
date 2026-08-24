package agent_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	"github.com/rodascaar/forgen/internal/adapters/out/fs"
	"github.com/rodascaar/forgen/internal/application/agent"
	"github.com/rodascaar/forgen/internal/application/session"
	"github.com/rodascaar/forgen/internal/application/tools"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// -- fakes --

type fakeProvider struct {
	streamFn func(ctx context.Context, request ports.ChatRequest, handler ports.StreamHandler) error
	calls    int
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) ListModels(ctx context.Context) ([]string, error) { return nil, nil }

func (f *fakeProvider) StreamChat(ctx context.Context, request ports.ChatRequest, handler ports.StreamHandler) error {
	f.calls++
	return f.streamFn(ctx, request, handler)
}

type memorySessionStore struct {
	sessions map[string]domain.Session
}

func newMemorySessionStore() *memorySessionStore {
	return &memorySessionStore{sessions: map[string]domain.Session{}}
}

func (m *memorySessionStore) Save(_ context.Context, session domain.Session) error {
	m.sessions[session.ID] = session
	return nil
}
func (m *memorySessionStore) Load(_ context.Context, id string) (domain.Session, error) {
	session, ok := m.sessions[id]
	if !ok {
		return domain.Session{}, fmt.Errorf("sesión %s no encontrada", id)
	}
	return session, nil
}
func (m *memorySessionStore) List(_ context.Context, limit int) ([]domain.Session, error) {
	result := make([]domain.Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		result = append(result, session)
	}
	return result, nil
}
func (m *memorySessionStore) Delete(_ context.Context, id string) error {
	delete(m.sessions, id)
	return nil
}

func (m *memorySessionStore) Export(_ context.Context, id string) ([]byte, error) {
	return nil, errors.New("no soportado")
}

func (m *memorySessionStore) Import(_ context.Context, data []byte) (domain.Session, error) {
	return domain.Session{}, errors.New("no soportado")
}

type recordingMessenger struct {
	streamed  []string
	toolCalls []string
	finalText string
}

func (r *recordingMessenger) StreamText(_ string, delta string) {
	r.streamed = append(r.streamed, delta)
}
func (r *recordingMessenger) ToolStarted(_ string, call domain.ToolCall) {
	r.toolCalls = append(r.toolCalls, call.Name)
}
func (r *recordingMessenger) ToolFinished(_ string, call domain.ToolCall, result domain.ToolResult) {}
func (r *recordingMessenger) Notice(_ string, text string)                                          {}
func (r *recordingMessenger) Error(_ string, err error)                                             {}
func (r *recordingMessenger) Finished(_ string, finalText string) {
	r.finalText = finalText
}

type allowResponder struct{}

func (allowResponder) Confirm(_ context.Context, _ string, _ domain.ToolCall) (bool, error) {
	return true, nil
}
func (allowResponder) Remember(_ context.Context, _ string, _ domain.ToolCall, _ domain.PermissionLevel) error {
	return nil
}

type alwaysDenyDecider struct{}

func (alwaysDenyDecider) Decide(_ context.Context, _ string, call domain.ToolCall) (domain.Decision, error) {
	return domain.Decision{Allowed: false, Level: domain.PermissionNever, Reason: "denegado en test"}, nil
}

func newTestRunner(t *testing.T, provider ports.LLMProvider, decider ports.PermissionDecider,
	responder ports.PermissionResponder, messenger ports.Messenger, store ports.SessionStore) *agent.Runner {
	t.Helper()

	fileSystem := fs.New(t.TempDir())
	registry := tools.NewRegistry(fileSystem, nilExecutor{}, nilGit{}, 1000)

	sessions := session.NewService(store)

	runner, err := agent.NewRunner(agent.Options{
		Provider:      provider,
		Tools:         registry,
		Decider:       decider,
		Responder:     responder,
		Messenger:     messenger,
		Sessions:      sessions,
		SystemPrompt:  func(ctx context.Context) (string, error) { return "system prompt", nil },
		MaxIterations: 5,
		Logger:        slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	return runner
}

type nilExecutor struct{}

func (nilExecutor) Execute(context.Context, string, string, []string) (ports.ExecResult, error) {
	return ports.ExecResult{}, errors.New("executor no disponible en test")
}

type nilGit struct{}

func (nilGit) Status(context.Context, string) (string, error) {
	return "", errors.New("git no disponible")
}
func (nilGit) Diff(context.Context, string, bool) (string, error) {
	return "", errors.New("git no disponible")
}
func (nilGit) IsRepo(context.Context, string) (bool, error) { return false, nil }

// -- tests --

func TestRunnerFinalText(t *testing.T) {
	provider := &fakeProvider{streamFn: func(ctx context.Context, request ports.ChatRequest, handler ports.StreamHandler) error {
		if err := handler(ports.TextDeltaEvent{Text: "Hola "}); err != nil {
			return err
		}
		if err := handler(ports.TextDeltaEvent{Text: "mundo"}); err != nil {
			return err
		}
		return handler(ports.DoneEvent{Reason: domain.FinishReasonStop})
	}}
	messenger := &recordingMessenger{}
	runner := newTestRunner(t, provider, allowAllDecider{}, allowResponder{}, messenger, newMemorySessionStore())

	session := domain.Session{
		ID:        "s1",
		Workspace: "/tmp",
		Model:     domain.Model{Provider: "fake", ID: "m"},
		Agent:     "build",
	}

	result, err := runner.Run(context.Background(), agent.RunInput{
		Session:    session,
		Agent:      domain.BuiltinAgents()[0],
		Workspace:  "/tmp",
		UserPrompt: "hola",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.FinalText != "Hola mundo" {
		t.Fatalf("FinalText = %q, want %q", result.FinalText, "Hola mundo")
	}
	if result.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1", result.Iterations)
	}
	if messenger.finalText != "Hola mundo" {
		t.Fatalf("messenger finalText = %q", messenger.finalText)
	}
}

func TestRunnerExecutesToolThenFinishes(t *testing.T) {
	provider := &fakeProvider{streamFn: func(ctx context.Context, request ports.ChatRequest, handler ports.StreamHandler) error {
		switch len(request.Messages) {
		case 2: // primera llamada: system + user -> devolver tool call
			return handler(ports.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Name: "glob", Arguments: map[string]any{"pattern": "*"}}})
		default: // segunda llamada: con resultado de herramienta -> texto final
			if err := handler(ports.TextDeltaEvent{Text: "listo"}); err != nil {
				return err
			}
			return handler(ports.DoneEvent{Reason: domain.FinishReasonStop})
		}
	}}

	// Registry real con un FileSystem que responde al glob.
	fileSystem := fs.New(t.TempDir())
	_ = fileSystem.Write(context.Background(), "a.txt", []byte("contenido"))
	registry := tools.NewRegistry(fileSystem, nilExecutor{}, nilGit{}, 1000)

	sessions := session.NewService(newMemorySessionStore())
	runner, err := agent.NewRunner(agent.Options{
		Provider:      provider,
		Tools:         registry,
		Decider:       allowAllDecider{},
		Responder:     allowResponder{},
		Messenger:     &recordingMessenger{},
		Sessions:      sessions,
		SystemPrompt:  func(ctx context.Context) (string, error) { return "system", nil },
		MaxIterations: 5,
		Logger:        slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	result, err := runner.Run(context.Background(), agent.RunInput{
		Session:    domain.Session{ID: "s2", Workspace: "/tmp", Model: domain.Model{Provider: "fake", ID: "m"}},
		Agent:      domain.BuiltinAgents()[0],
		Workspace:  "/tmp",
		UserPrompt: "lista archivos",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.FinalText != "listo" {
		t.Fatalf("FinalText = %q", result.FinalText)
	}
	if result.ToolCalls != 1 {
		t.Fatalf("ToolCalls = %d, want 1", result.ToolCalls)
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.calls)
	}
}

func TestRunnerDeniedToolDoesNotExecute(t *testing.T) {
	// El proveedor pide ejecutar bash y luego, con el resultado denegado,
	// responde con texto final (sin ejecutar la herramienta).
	provider := &fakeProvider{streamFn: func(ctx context.Context, request ports.ChatRequest, handler ports.StreamHandler) error {
		if len(request.Messages) <= 3 {
			return handler(ports.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Name: "bash", Arguments: map[string]any{"command": "rm -rf /"}}})
		}
		if err := handler(ports.TextDeltaEvent{Text: "no ejecutado"}); err != nil {
			return err
		}
		return handler(ports.DoneEvent{Reason: domain.FinishReasonStop})
	}}

	decider := alwaysDenyDecider{}
	fileSystem := fs.New(t.TempDir())
	registry := tools.NewRegistry(fileSystem, nilExecutor{}, nilGit{}, 1000)

	sessions := session.NewService(newMemorySessionStore())
	runner, err := agent.NewRunner(agent.Options{
		Provider:      provider,
		Tools:         registry,
		Decider:       decider,
		Responder:     allowResponder{},
		Messenger:     &recordingMessenger{},
		Sessions:      sessions,
		SystemPrompt:  func(ctx context.Context) (string, error) { return "system", nil },
		MaxIterations: 5,
		Logger:        slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	result, err := runner.Run(context.Background(), agent.RunInput{
		Session:    domain.Session{ID: "s3", Workspace: "/tmp", Model: domain.Model{Provider: "fake", ID: "m"}},
		Agent:      domain.BuiltinAgents()[0],
		Workspace:  "/tmp",
		UserPrompt: "borra todo",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.FinalText != "no ejecutado" {
		t.Fatalf("FinalText = %q", result.FinalText)
	}
}

func TestRunnerMaxIterationsGuard(t *testing.T) {
	provider := &fakeProvider{streamFn: func(ctx context.Context, request ports.ChatRequest, handler ports.StreamHandler) error {
		// Siempre devuelve una llamada a herramienta: nunca termina.
		return handler(ports.ToolCallEvent{Call: domain.ToolCall{ID: "c1", Name: "glob", Arguments: map[string]any{"pattern": "*"}}})
	}}

	fileSystem := fs.New(t.TempDir())
	registry := tools.NewRegistry(fileSystem, nilExecutor{}, nilGit{}, 1000)

	sessions := session.NewService(newMemorySessionStore())
	runner, err := agent.NewRunner(agent.Options{
		Provider:      provider,
		Tools:         registry,
		Decider:       allowAllDecider{},
		Responder:     allowResponder{},
		Messenger:     &recordingMessenger{},
		Sessions:      sessions,
		SystemPrompt:  func(ctx context.Context) (string, error) { return "system", nil },
		MaxIterations: 3,
		Logger:        slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	result, err := runner.Run(context.Background(), agent.RunInput{
		Session:    domain.Session{ID: "s4", Workspace: "/tmp", Model: domain.Model{Provider: "fake", ID: "m"}},
		Agent:      domain.BuiltinAgents()[0],
		Workspace:  "/tmp",
		UserPrompt: "loop",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Iterations != 3 {
		t.Fatalf("Iterations = %d, want 3 (guard)", result.Iterations)
	}
}

type allowAllDecider struct{}

func (allowAllDecider) Decide(_ context.Context, _ string, _ domain.ToolCall) (domain.Decision, error) {
	return domain.Decision{Allowed: true, Level: domain.PermissionAuto, Reason: "test"}, nil
}

// TestReadOnlyAgentSeesOnlyReadOnlyTools verifica que el agente plan (read-only)
// solo recibe herramientas de lectura/exploración, y nunca herramientas que
// puedan modificar el sistema (write, edit, bash, apply_patch, task, lsp_rename,
// todo) aunque el proveedor las pidiera.
func TestReadOnlyAgentSeesOnlyReadOnlyTools(t *testing.T) {
	var toolsSeen []domain.Tool
	provider := &fakeProvider{streamFn: func(ctx context.Context, request ports.ChatRequest, handler ports.StreamHandler) error {
		toolsSeen = request.Tools
		if err := handler(ports.TextDeltaEvent{Text: "plan listo"}); err != nil {
			return err
		}
		return handler(ports.DoneEvent{Reason: domain.FinishReasonStop})
	}}

	fileSystem := fs.New(t.TempDir())
	registry := tools.NewRegistry(fileSystem, nilExecutor{}, nilGit{}, 1000)
	sessions := session.NewService(newMemorySessionStore())
	runner, err := agent.NewRunner(agent.Options{
		Provider:      provider,
		Tools:         registry,
		Decider:       allowAllDecider{},
		Responder:     allowResponder{},
		Messenger:     &recordingMessenger{},
		Sessions:      sessions,
		SystemPrompt:  func(ctx context.Context) (string, error) { return "system", nil },
		MaxIterations: 5,
		Logger:        slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	planAgent, ok := domain.FindAgent(domain.BuiltinAgents(), "plan")
	if !ok || !planAgent.IsReadOnly {
		t.Fatalf("el agente plan debería existir y ser read-only")
	}

	_, err = runner.Run(context.Background(), agent.RunInput{
		Session:    domain.Session{ID: "s5", Workspace: "/tmp", Model: domain.Model{Provider: "fake", ID: "m"}},
		Agent:      planAgent,
		Workspace:  "/tmp",
		UserPrompt: "plan",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	seen := make(map[string]bool, len(toolsSeen))
	for _, tool := range toolsSeen {
		seen[tool.Name] = true
	}

	for _, mutating := range []string{"write", "edit", "bash", "apply_patch", "task", "lsp_rename", "todo"} {
		if seen[mutating] {
			t.Fatalf("el agente plan NO debería ver la herramienta %q", mutating)
		}
	}
	for _, readOnly := range []string{"read", "glob", "grep", "git_status", "git_diff"} {
		if !seen[readOnly] {
			t.Fatalf("el agente plan debería ver la herramienta de lectura %q", readOnly)
		}
	}
}
