// Package agent contiene el caso de uso principal: el loop del agente.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/forgen/forgen/internal/application/session"
	"github.com/forgen/forgen/internal/core/domain"
	"github.com/forgen/forgen/internal/core/ports"
)

// const defaultTemperature = 0.2

const (
	defaultTemperature  = 0.2
	defaultMaxTokens    = 4096
	deniedResultMessage = "PERMISO DENEGADO"
)

// Runner ejecuta un turno completo del agente: prompt → LLM → tools → observación.
type Runner struct {
	provider      ports.LLMProvider
	tools         ports.ToolExecutor
	decider       ports.PermissionDecider
	responder     ports.PermissionResponder
	messenger     ports.Messenger
	sessions      *session.Service
	systemPrompt  func(context.Context) (string, error)
	usage         ports.UsageRecorder
	maxIterations int
	logger        *slog.Logger
}

// RunInput agrupa los datos de entrada de un turno.
type RunInput struct {
	Session    domain.Session
	Agent      domain.Agent
	Workspace  string
	UserPrompt string
	Phase      domain.AgentPhase
}

// RunResult resume el resultado de un turno.
type RunResult struct {
	Session    domain.Session
	FinalText  string
	Iterations int
	ToolCalls  int
}

// Options configura el Runner.
type Options struct {
	Provider      ports.LLMProvider
	Tools         ports.ToolExecutor
	Decider       ports.PermissionDecider
	Responder     ports.PermissionResponder
	Messenger     ports.Messenger
	Sessions      *session.Service
	SystemPrompt  func(context.Context) (string, error)
	Usage         ports.UsageRecorder
	MaxIterations int
	Logger        *slog.Logger
}

// NewRunner construye el Runner validando que no falten dependencias (fail-fast).
func NewRunner(options Options) (*Runner, error) {
	if options.Provider == nil {
		return nil, errors.New("runner: falta el provider LLM")
	}
	if options.Tools == nil {
		return nil, errors.New("runner: falta el ejecutor de herramientas")
	}
	if options.Decider == nil {
		return nil, errors.New("runner: falta el decisor de permisos")
	}
	if options.Responder == nil {
		return nil, errors.New("runner: falta el respondedor de permisos")
	}
	if options.Messenger == nil {
		return nil, errors.New("runner: falta el messenger")
	}
	if options.Sessions == nil {
		return nil, errors.New("runner: falta el servicio de sesiones")
	}
	if options.SystemPrompt == nil {
		return nil, errors.New("runner: falta el builder de system prompt")
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	maxIterations := options.MaxIterations
	if maxIterations <= 0 {
		maxIterations = 50
	}
	return &Runner{
		provider:      options.Provider,
		tools:         options.Tools,
		decider:       options.Decider,
		responder:     options.Responder,
		messenger:     options.Messenger,
		sessions:      options.Sessions,
		systemPrompt:  options.SystemPrompt,
		usage:         options.Usage,
		maxIterations: maxIterations,
		logger:        options.Logger,
	}, nil
}

// Run ejecuta el loop del agente hasta respuesta final o máximo de iteraciones.
func (r *Runner) Run(ctx context.Context, input RunInput) (RunResult, error) {
	// 1. Persistir el mensaje de usuario y construir el contexto.
	sessionResult, err := r.sessions.AppendMessage(ctx, input.Session,
		domain.NewTextMessage(domain.RoleUser, input.UserPrompt))
	if err != nil {
		return RunResult{}, err
	}
	input.Session = sessionResult

	// 2. System prompt estático (agente + contexto de proyecto).
	systemPrompt, err := r.systemPrompt(ctx)
	if err != nil {
		return RunResult{}, fmt.Errorf("construir system prompt: %w", err)
	}

	// 3. Herramientas visibles según el agente.
	tools := r.visibleTools(input.Agent)

	totalToolCalls := 0
	for iteration := 0; iteration < r.maxIterations; iteration++ {
		messages := r.buildMessages(systemPrompt, input.Session)

		// Emitir eventos de observabilidad.
		r.logger.Info("llm.request", "session", input.Session.ID, "iteration", iteration,
			"model", input.Session.Model.Key(), "messages", len(messages), "tools", len(tools))

		response, err := r.callLLM(ctx, input.Session.ID, input.Session.Model, messages, tools, input.Phase)
		if err != nil {
			return RunResult{}, err
		}
		totalToolCalls += response.toolCallCount

		if !response.hasToolCalls {
			// Respuesta final de texto.
			assistantMessage := domain.NewTextMessage(domain.RoleAssistant, response.text)
			updated, err := r.sessions.AppendMessage(ctx, input.Session, assistantMessage)
			if err != nil {
				return RunResult{}, err
			}
			input.Session = updated
			r.messenger.Finished(input.Session.ID, response.text)
			return RunResult{
				Session:    input.Session,
				FinalText:  response.text,
				Iterations: iteration + 1,
				ToolCalls:  totalToolCalls,
			}, nil
		}

		// Ejecutar herramientas y recoger resultados.
		assistantMessage := domain.NewAssistantWithToolCalls(response.text, response.toolCalls)
		toolMessages, err := r.executeTools(ctx, input.Session.ID, input.Workspace, response.toolCalls)
		if err != nil {
			return RunResult{}, err
		}
		input.Session.Messages = append(input.Session.Messages, assistantMessage)
		input.Session.Messages = append(input.Session.Messages, toolMessages...)
		if err := r.sessions.Save(ctx, input.Session); err != nil {
			return RunResult{}, err
		}
	}

	// Guard: se agotaron las iteraciones.
	message := fmt.Sprintf("Se alcanzó el máximo de %d iteraciones sin respuesta final.", r.maxIterations)
	r.messenger.Notice(input.Session.ID, message)
	r.logger.Warn("agent.max_iterations", "session", input.Session.ID)
	return RunResult{
		Session:    input.Session,
		FinalText:  message,
		Iterations: r.maxIterations,
		ToolCalls:  totalToolCalls,
	}, nil
}

// llmResponse acumula la respuesta del proveedor durante el streaming.
type llmResponse struct {
	text          string
	toolCalls     []domain.ToolCall
	hasToolCalls  bool
	toolCallCount int
	usage         domain.Usage
}

func (r *Runner) callLLM(ctx context.Context, sessionID string, model domain.Model,
	messages []domain.Message, tools []domain.Tool, phase domain.AgentPhase) (llmResponse, error) {
	var response llmResponse
	var mutex sync.Mutex

	request := ports.ChatRequest{
		Model:       model,
		Messages:    messages,
		Tools:       tools,
		Temperature: defaultTemperature,
		MaxTokens:   defaultMaxTokens,
	}

	err := r.provider.StreamChat(ctx, request, func(event ports.StreamEvent) error {
		switch typedEvent := event.(type) {
		case ports.TextDeltaEvent:
			mutex.Lock()
			response.text += typedEvent.Text
			mutex.Unlock()
			r.messenger.StreamText(sessionID, typedEvent.Text)
		case ports.ToolCallEvent:
			mutex.Lock()
			response.toolCalls = append(response.toolCalls, typedEvent.Call)
			response.toolCallCount++
			response.hasToolCalls = true
			mutex.Unlock()
		case ports.UsageEvent:
			mutex.Lock()
			response.usage = typedEvent.Usage
			mutex.Unlock()
		case ports.DoneEvent:
			r.logger.Info("llm.response", "session", sessionID, "reason", typedEvent.Reason)
		case ports.ErrorEvent:
			r.logger.Error("llm.stream_error", "session", sessionID, "err", typedEvent.Err)
		}
		return nil
	})
	if err != nil {
		return llmResponse{}, fmt.Errorf("llm %s/%s: %w", model.Provider, model.ID, err)
	}
	if response.toolCallCount > 0 && len(response.toolCalls) != response.toolCallCount {
		return llmResponse{}, errors.New("llm: llamadas a herramientas incompletas en el streaming")
	}
	r.recordUsage(ctx, sessionID, model, phase, response.usage)
	return response, nil
}

// recordUsage persiste el consumo de tokens si hay un recorder configurado.
func (r *Runner) recordUsage(ctx context.Context, sessionID string, model domain.Model, phase domain.AgentPhase, usage domain.Usage) {
	if r.usage == nil {
		return
	}
	record := domain.UsageRecord{
		SessionID:    sessionID,
		Provider:     model.Provider,
		Model:        model.ID,
		Phase:        string(phase),
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
	}
	if err := r.usage.Record(ctx, record); err != nil {
		r.logger.Warn("no se pudo registrar uso", "err", err)
	}
}

// executeTools decide permisos, ejecuta y construye los mensajes de resultado.
func (r *Runner) executeTools(ctx context.Context, sessionID, workspace string,
	calls []domain.ToolCall) ([]domain.Message, error) {
	toolMessages := make([]domain.Message, 0, len(calls))

	for _, call := range calls {
		r.messenger.ToolStarted(sessionID, call)
		result := r.executeWithPermission(ctx, sessionID, call)
		r.messenger.ToolFinished(sessionID, call, result)
		r.logger.Info("tool.finished", "session", sessionID, "tool", call.Name,
			"ok", result.OK, "error", errorString(result.Error))
		toolMessages = append(toolMessages, domain.NewToolResultMessage(call.ID, call.Name, result))
	}
	return toolMessages, nil
}

func (r *Runner) executeWithPermission(ctx context.Context, sessionID string, call domain.ToolCall) domain.ToolResult {
	decision, err := r.decider.Decide(ctx, sessionID, call)
	if err != nil {
		return domain.ToolResult{ToolCallID: call.ID, OK: false, Error: fmt.Errorf("decidir permiso: %w", err)}
	}

	if !decision.Allowed && decision.Level == domain.PermissionOnRequest {
		allowed, confirmErr := r.responder.Confirm(ctx, sessionID, call)
		if confirmErr != nil {
			return domain.ToolResult{ToolCallID: call.ID, OK: false, Error: confirmErr}
		}
		if allowed {
			decision = domain.Decision{Allowed: true, Level: domain.PermissionOnRequest, Reason: "confirmado por usuario"}
		}
	}

	if !decision.Allowed {
		r.messenger.Notice(sessionID, fmt.Sprintf("Permiso denegado para %s: %s", call.Name, decision.Reason))
		return domain.ToolResult{
			ToolCallID: call.ID,
			OK:         false,
			Output:     deniedResultMessage,
			Error:      fmt.Errorf("%s: %s", deniedResultMessage, decision.Reason),
		}
	}

	return r.tools.Execute(ctx, call)
}

// visibleTools filtra las herramientas según el agente.
func (r *Runner) visibleTools(agent domain.Agent) []domain.Tool {
	if agent.IsReadOnly {
		// El agente de solo lectura no ve herramientas de escritura/ejecución.
		readOnly := make([]domain.Tool, 0)
		for _, tool := range r.tools.ListTools() {
			switch tool.Name {
			case "write", "edit", "bash":
				continue
			}
			readOnly = append(readOnly, tool)
		}
		return readOnly
	}
	return r.tools.LookupTools(agent.AllowedTools)
}

// buildMessages construye la lista de mensajes para el proveedor.
func (r *Runner) buildMessages(systemPrompt string, session domain.Session) []domain.Message {
	messages := make([]domain.Message, 0, len(session.Messages)+1)
	messages = append(messages, domain.NewTextMessage(domain.RoleSystem, systemPrompt))
	messages = append(messages, session.Messages...)
	return messages
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// TrimSystemPrompt evita que el contexto supere el límite de tokens del sistema.
func TrimSystemPrompt(prompt string, maxChars int) string {
	if len(prompt) <= maxChars {
		return prompt
	}
	return strings.TrimSpace(prompt[:maxChars]) + "\n... [contexto truncado]"
}
