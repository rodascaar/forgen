package task

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// ProviderResolver resuelve el LLM provider y modelo para una tarea.
// Inyectado por App para evitar ciclo de imports y usar orquestación real.
type ProviderResolver func(ctx context.Context, task *domain.Task) (ports.LLMProvider, domain.Model, error)

// RunnerFactory crea un loop aislado con tools filtradas por el subagente.
// Si es nil, runAgent cae al fallback de 1-shot StreamChat.
type RunnerFactory func(ctx context.Context, task *domain.Task) (Runner, error)

// Runner es la interfaz mínima del AgentRunner usada por subagentes.
type Runner interface {
	Run(ctx context.Context, workspace string, prompt string) (string, error)
}

type ExecutorDeps struct {
	LLMFactory       ports.LLMProviderFactory
	Credentials      ports.CredentialStore
	ProviderResolver ProviderResolver
	RunnerFactory    RunnerFactory
}

type Executor struct {
	mu    sync.RWMutex
	tasks map[string]*domain.Task
	deps  ExecutorDeps
	store ports.TaskStore
}

func NewExecutor(deps ExecutorDeps, store ports.TaskStore) *Executor {
	return &Executor{tasks: make(map[string]*domain.Task), deps: deps, store: store}
}

// SetProviderResolver permite a App inyectar resolución orquestada sin ciclo de imports.
func (e *Executor) SetProviderResolver(resolver ProviderResolver) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.deps.ProviderResolver = resolver
}

// SetRunnerFactory inyecta el factory de Runner aislado (con tools filtradas).
func (e *Executor) SetRunnerFactory(factory RunnerFactory) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.deps.RunnerFactory = factory
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
	// Si hay RunnerFactory, delega al loop aislado con tools (build plan real).
	e.mu.RLock()
	factory := e.deps.RunnerFactory
	e.mu.RUnlock()
	if factory != nil {
		runner, err := factory(ctx, task)
		if err != nil {
			return nil, fmt.Errorf("crear runner subagente: %w", err)
		}
		// Workspace por defecto: cwd actual (inyectado por App vía closure si quiere override)
		workspace := "."
		output, err := runner.Run(ctx, workspace, task.Description)
		if err != nil {
			return nil, err
		}
		result := &domain.TaskResult{
			Summary:      fmt.Sprintf("Tarea '%s' completada", task.Name),
			Output:       output,
			FilesChanged: []string{},
			Artifacts:    map[string]any{},
		}
		if task.StartedAt != nil {
			result.Metrics = map[string]any{"duration_ms": time.Since(*task.StartedAt).Milliseconds()}
		}
		return result, nil
	}

	var provider ports.LLMProvider
	var model domain.Model
	var err error

	// Resolver provider/modelo vía inyección (orquestación) o fallback corregido.
	e.mu.RLock()
	resolver := e.deps.ProviderResolver
	e.mu.RUnlock()
	if resolver != nil {
		provider, model, err = resolver(ctx, task)
		if err != nil {
			return nil, fmt.Errorf("resolver provider para tarea: %w", err)
		}
	} else {
		// Fallback: usa DefaultAppConfig y resuelve credencial correctamente por nombre.
		provider, err = e.deps.LLMFactory.CreateWithKeyResolver(
			domain.ProviderConfig{Name: "openai", Type: domain.ProviderTypeOpenAICompatible, BaseURL: "https://api.openai.com/v1", APIKeyEnv: "OPENAI_API_KEY"},
			func(cfg domain.ProviderConfig) string {
				if e.deps.Credentials != nil {
					if secret, err := e.deps.Credentials.Get(ctx, "providers/"+cfg.Name); err == nil && secret != "" {
						return secret
					}
				}
				// Fallback a env (para tests)
				return cfg.ResolveAPIKey(func(k string) string {
					// os.Getenv no disponible aquí sin import, devolver vacío y dejar que factory use env externo
					return ""
				})
			}, nil)
		if err != nil {
			return nil, fmt.Errorf("crear provider: %w", err)
		}
		model = domain.Model{Provider: "openai", ID: "gpt-5"}
	}

	// System prompt del subagente + descripción como usuario (fallback 1-shot).
	systemText := task.Config.Prompt
	if systemText == "" {
		systemText = "Eres un subagente especializado en " + string(task.Type) + ". Resuelve la tarea de forma autónoma."
	}
	messages := []domain.Message{
		{Role: domain.RoleSystem, Content: []domain.ContentPart{{Type: "text", Text: systemText}}},
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
	req := ports.ChatRequest{Model: model, Messages: messages, Temperature: 0.2, MaxTokens: 4096}
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
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return e.store.Save(saveCtx, t)
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
