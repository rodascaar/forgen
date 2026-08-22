package task

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

type ExecutorDeps struct {
	LLMFactory  ports.LLMProviderFactory
	Credentials ports.CredentialStore
}

type Executor struct {
	mu    sync.RWMutex
	tasks map[string]*domain.Task
	deps ExecutorDeps
	store ports.TaskStore
}

func NewExecutor(deps ExecutorDeps, store ports.TaskStore) *Executor {
	return &Executor{tasks: make(map[string]*domain.Task), deps: deps, store: store}
}

func (e *Executor) Execute(ctx context.Context, task *domain.Task) (*domain.TaskResult, error) {
	return e.ExecuteWithConfig(ctx, task, task.Config)
}

func (e *Executor) ExecuteWithConfig(ctx context.Context, task *domain.Task, _ domain.SubAgentConfig) (*domain.TaskResult, error) {
	e.mu.Lock()
	task.MarkRunning()
	e.tasks[task.ID] = task
	e.mu.Unlock()
	if err := e.store.Save(ctx, task); err != nil {
		return nil, fmt.Errorf("guardar tarea: %w", err)
	}
	ctx2, cancel := context.WithTimeout(ctx, time.Duration(task.Config.Timeout)*time.Second)
	defer cancel()
	result, err := e.runAgent(ctx2, task)
	e.mu.Lock()
	if err != nil {
		task.MarkFailed(err.Error())
	} else {
		task.MarkCompleted(result)
	}
	e.mu.Unlock()
	if err := e.store.Save(ctx, task); err != nil {
		return nil, fmt.Errorf("guardar resultado: %w", err)
	}
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (e *Executor) runAgent(ctx context.Context, task *domain.Task) (*domain.TaskResult, error) {
	provider, err := e.deps.LLMFactory.CreateWithKeyResolver(
		domain.ProviderConfig{Name: "default", Type: domain.ProviderTypeOpenAICompatible, BaseURL: "https://api.openai.com/v1", APIKeyEnv: "OPENAI_API_KEY"},
		func(cfg domain.ProviderConfig) string {
			cred, _ := e.deps.Credentials.Get(ctx, cfg.BaseURL)
			return cred
		}, nil)
	if err != nil {
		return nil, fmt.Errorf("crear provider: %w", err)
	}
	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: []domain.ContentPart{{Type: "text", Text: task.Config.Prompt}}},
		{Role: domain.RoleUser, Content: []domain.ContentPart{{Type: "text", Text: task.Description}}},
	}
	var result *domain.TaskResult
	handler := func(event ports.StreamEvent) error {
		switch ev := event.(type) {
		case ports.TextDeltaEvent:
			if result == nil {
				result = &domain.TaskResult{}
			}
			result.Output += ev.Text
		case ports.DoneEvent:
			if result != nil {
				result.Summary = fmt.Sprintf("Tarea '%s' completada", task.Name)
			}
		case ports.ErrorEvent:
			return ev.Err
		}
		return nil
	}
	req := ports.ChatRequest{Model: domain.Model{Provider: "openai", ID: "gpt-4"}, Messages: messages, Temperature: 0.2, MaxTokens: 4096}
	if err := provider.StreamChat(ctx, req, handler); err != nil {
		return nil, err
	}
	if result == nil {
		result = &domain.TaskResult{Summary: fmt.Sprintf("Tarea '%s' completada", task.Name)}
	}
	result.FilesChanged = []string{}
	result.Artifacts = map[string]any{}
	if task.StartedAt != nil {
		result.Metrics = map[string]any{"duration_ms": time.Since(*task.StartedAt).Milliseconds()}
	}
	return result, nil
}

func (e *Executor) Cancel(ctx context.Context, taskID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, ok := e.tasks[taskID]
	if !ok {
		return fmt.Errorf("tarea no encontrada: %s", taskID)
	}
	if t.IsTerminal() {
		return fmt.Errorf("la tarea ya ha terminado: %s", t.Status)
	}
	t.MarkCancelled()
	return e.store.Save(context.Background(), t)
}

func (e *Executor) GetStatus(ctx context.Context, taskID string) (domain.TaskStatus, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	t, ok := e.tasks[taskID]
	if !ok {
		t2, err := e.store.Load(ctx, taskID)
		if err != nil {
			return "", fmt.Errorf("tarea no encontrada: %s", taskID)
		}
		return t2.Status, nil
	}
	return t.Status, nil
}

func (e *Executor) GetTask(taskID string) (*domain.Task, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	t, ok := e.tasks[taskID]
	return t, ok
}

func (e *Executor) ListTasks(ctx context.Context, parentID *string) ([]*domain.Task, error) {
	return e.store.List(ctx, parentID)
}
