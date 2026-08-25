// Package agent contiene el caso de uso principal: el loop del agente.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rodascaar/forgen/internal/application/memory"
	"github.com/rodascaar/forgen/internal/application/session"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// const defaultTemperature = 0.2

const (
	defaultTemperature  = 0.2
	defaultMaxTokens    = 4096
	deniedResultMessage = "PERMISO DENEGADO"
)

// Runner ejecuta un turno completo del agente: prompt → LLM → tools → observación.
type Runner struct {
	provider        ports.LLMProvider
	tools           ports.ToolExecutor
	decider         ports.PermissionDecider
	responder       ports.PermissionResponder
	messenger       ports.Messenger
	sessions        *session.Service
	systemPrompt    func(context.Context) (string, error)
	usage           ports.UsageRecorder
	maxIterations   int
	reasoningEffort string
	compaction      CompactionConfig
	diagnostics     func(context.Context, string) string
	logger          *slog.Logger
}

// CompactionConfig controla auto-compaction en el Runner.
type CompactionConfig struct {
	Threshold     float64
	Disabled      bool
	ModelMetadata map[string]domain.ModelMetadata
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
	Provider        ports.LLMProvider
	Tools           ports.ToolExecutor
	Decider         ports.PermissionDecider
	Responder       ports.PermissionResponder
	Messenger       ports.Messenger
	Sessions        *session.Service
	SystemPrompt    func(context.Context) (string, error)
	Usage           ports.UsageRecorder
	MaxIterations   int
	ReasoningEffort string
	Compaction      CompactionConfig
	Diagnostics     func(context.Context, string) string
	Logger          *slog.Logger
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
		provider:        options.Provider,
		tools:           options.Tools,
		decider:         options.Decider,
		responder:       options.Responder,
		messenger:       options.Messenger,
		sessions:        options.Sessions,
		systemPrompt:    options.SystemPrompt,
		usage:           options.Usage,
		maxIterations:   maxIterations,
		reasoningEffort: options.ReasoningEffort,
		compaction:      options.Compaction,
		diagnostics:     options.Diagnostics,
		logger:          options.Logger,
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

	// 3.1 Auto-compaction pre-turn si ya estamos en overflow (evita prompt_too_long).
	if err := r.maybeCompact(ctx, &input.Session, ""); err != nil {
		r.logger.Warn("auto-compact pre-turn", "err", err)
	}

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
		toolMessages, err := r.executeTools(ctx, input.Session.ID, input.Workspace, input.Agent, response.toolCalls)
		if err != nil {
			return RunResult{}, err
		}
		input.Session.Messages = append(input.Session.Messages, assistantMessage)
		input.Session.Messages = append(input.Session.Messages, toolMessages...)
		if err := r.sessions.Save(ctx, input.Session); err != nil {
			return RunResult{}, err
		}
		// Auto-compaction tras tool results si superamos umbral (no bloquea si falla).
		if err := r.maybeCompact(ctx, &input.Session, ""); err != nil {
			r.logger.Warn("auto-compact post-tools", "err", err)
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

// llmTimeout acota cada llamada al proveedor para que una API colgada no
// deje el spinner girando indefinidamente ni bloquee la cancelación.
const llmTimeout = 150 * time.Second

func (r *Runner) callLLM(ctx context.Context, sessionID string, model domain.Model,
	messages []domain.Message, tools []domain.Tool, phase domain.AgentPhase) (llmResponse, error) {
	var response llmResponse
	var mutex sync.Mutex
	// Token budget & cost guard (7.6.3) — estima y warn si >80%
	budgetTokens := 0
	for _, m := range messages {
		budgetTokens += len(m.Text())/4 + 4
	}
	if budgetTokens > 90000 {
		r.logger.Warn("token.budget_high", "session", sessionID, "tokens", budgetTokens, "hint", "consider /compact or fresh session")
		r.messenger.Notice(sessionID, fmt.Sprintf("⚠ Tokens altos: %d — considera /compact o sesión nueva para ahorrar coste", budgetTokens))
	}

	ctx, cancel := context.WithTimeout(ctx, llmTimeout)
	defer cancel()

	request := ports.ChatRequest{
		Model:           model,
		Messages:        messages,
		Tools:           tools,
		Temperature:     defaultTemperature,
		MaxTokens:       defaultMaxTokens,
		ReasoningEffort: r.reasoningEffort,
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

// executeTools decide permisos y ejecuta con paralelismo limitado (5 concurrent, cache-friendly).
// Doom-loop guard: si 3× mismo tool+args, injecta warning.
// Mantiene orden de toolMessages igual a calls para correlación con LLM.
func (r *Runner) executeTools(ctx context.Context, sessionID, workspace string, agent domain.Agent,
	calls []domain.ToolCall) ([]domain.Message, error) {
	if doom := r.detectDoomLoop(calls); doom != "" {
		r.messenger.Notice(sessionID, doom)
		r.logger.Warn("tool.doom_loop", "session", sessionID, "hint", doom)
	}
	// Fase permisos secuencial (Confirm interactivo no paralelizable), luego ejecución paralela donde sea seguro.
	type permResult struct {
		idx    int
		call   domain.ToolCall
		result *domain.ToolResult // non-nil si permiso denegado o readOnly block (no ejecutar)
	}
	permResults := make([]permResult, len(calls))
	for i, call := range calls {
		// Check readOnly block primero (sync, sin I/O)
		if agent.IsReadOnly && !readOnlyToolAllowlist[call.Name] {
			denied := domain.ToolResult{
				ToolCallID: call.ID,
				OK:         false,
				Output:     deniedResultMessage,
				Error:      fmt.Errorf("%s: herramienta %q no permitida en modo plan", deniedResultMessage, call.Name),
			}
			permResults[i] = permResult{idx: i, call: call, result: &denied}
			continue
		}
		decision, err := r.decider.Decide(ctx, sessionID, call)
		if err != nil {
			denied := domain.ToolResult{ToolCallID: call.ID, OK: false, Error: fmt.Errorf("decidir permiso: %w", err)}
			permResults[i] = permResult{idx: i, call: call, result: &denied}
			continue
		}
		if !decision.Allowed && decision.Level == domain.PermissionOnRequest {
			allowed, confirmErr := r.responder.Confirm(ctx, sessionID, call)
			if confirmErr != nil {
				denied := domain.ToolResult{ToolCallID: call.ID, OK: false, Error: confirmErr}
				permResults[i] = permResult{idx: i, call: call, result: &denied}
				continue
			}
			if allowed {
				decision = domain.Decision{Allowed: true, Level: domain.PermissionOnRequest, Reason: "confirmado por usuario"}
			}
		}
		if !decision.Allowed {
			r.messenger.Notice(sessionID, fmt.Sprintf("Permiso denegado para %s: %s", call.Name, decision.Reason))
			denied := domain.ToolResult{
				ToolCallID: call.ID,
				OK:         false,
				Output:     deniedResultMessage,
				Error:      fmt.Errorf("%s: %s", deniedResultMessage, decision.Reason),
			}
			permResults[i] = permResult{idx: i, call: call, result: &denied}
			continue
		}
		permResults[i] = permResult{idx: i, call: call, result: nil}
	}

	// Ejecutar en paralelo con límite 5
	toolMessages := make([]domain.Message, len(calls))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)
	for i, pr := range permResults {
		if pr.result != nil {
			// Permiso denegado — no ejecuta, síncrono
			r.messenger.ToolStarted(sessionID, pr.call)
			r.messenger.ToolFinished(sessionID, pr.call, *pr.result)
			r.logger.Info("tool.finished", "session", sessionID, "tool", pr.call.Name, "ok", pr.result.OK, "error", errorString(pr.result.Error))
			toolMessages[i] = domain.NewToolResultMessage(pr.call.ID, pr.call.Name, *pr.result)
			continue
		}
		wg.Add(1)
		go func(idx int, call domain.ToolCall) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			r.messenger.ToolStarted(sessionID, call)
			result := r.tools.Execute(ctx, call)
			result.ToolCallID = call.ID
			// PostToolUse diagnostics (7.5.2) — feed LSP diagnostics after write/edit/patch
			if (call.Name == "write" || call.Name == "edit" || call.Name == "apply_patch") && r.diagnostics != nil {
				if p, ok := call.Arguments["path"].(string); ok && p != "" {
					if diag := r.diagnostics(ctx, p); diag != "" && diag != "(sin diagnósticos)" {
						result.Output += "\n[LSP diagnostics for " + p + "]\n" + diag
						r.messenger.Notice(sessionID, "LSP diagnostics: "+diag)
					}
				} else if patch, ok := call.Arguments["patch"].(string); ok && r.diagnostics != nil {
					// try to extract file from patch header
					if extracted := extractPatchPath(patch); extracted != "" {
						if diag := r.diagnostics(ctx, extracted); diag != "" && diag != "(sin diagnósticos)" {
							result.Output += "\n[LSP diagnostics for " + extracted + "]\n" + diag
						}
					}
				}
			}
			r.messenger.ToolFinished(sessionID, call, result)
			r.logger.Info("tool.finished", "session", sessionID, "tool", call.Name, "ok", result.OK, "error", errorString(result.Error))
			toolMessages[idx] = domain.NewToolResultMessage(call.ID, call.Name, result)
		}(i, pr.call)
	}
	wg.Wait()
	return toolMessages, nil
}

// doom-loop: 3× mismo tool+args exactos
var doomHistory = struct {
	sync.Mutex
	recent []string
}{}

func (r *Runner) detectDoomLoop(calls []domain.ToolCall) string {
	key := func(c domain.ToolCall) string {
		return c.Name + ":" + fmt.Sprintf("%v", c.Arguments)
	}
	doomHistory.Lock()
	defer doomHistory.Unlock()
	for _, c := range calls {
		k := key(c)
		doomHistory.recent = append(doomHistory.recent, k)
		if len(doomHistory.recent) > 10 {
			doomHistory.recent = doomHistory.recent[len(doomHistory.recent)-10:]
		}
		count := 0
		for _, r := range doomHistory.recent {
			if r == k {
				count++
			}
		}
		if count >= 3 {
			return fmt.Sprintf("⚠ Doom-loop detectado: '%s' repetido %dx — prueba alternativa (glob vs grep, read_many_files, o cambia args).", c.Name, count)
		}
	}
	return ""
}

func (r *Runner) executeWithPermission(ctx context.Context, sessionID string, agent domain.Agent, call domain.ToolCall) domain.ToolResult {
	// Red de seguridad del modo plan: aunque el LLM pidiera una herramienta
	// mutadora, un agente de solo lectura nunca puede ejecutarla.
	if agent.IsReadOnly && !readOnlyToolAllowlist[call.Name] {
		return domain.ToolResult{
			ToolCallID: call.ID,
			OK:         false,
			Output:     deniedResultMessage,
			Error:      fmt.Errorf("%s: herramienta %q no permitida en modo plan", deniedResultMessage, call.Name),
		}
	}

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

// readOnlyToolAllowlist es el conjunto de herramientas permitidas a los
// agentes de solo lectura (modo plan): solo lectura/exploración e investigación
// (logs, búsqueda en la web, git status/diff y LSP de lectura). Cualquier
// herramienta capaz de modificar archivos, estado o lanzar sub-agentes
// (task, write, edit, bash, apply_patch, lsp_rename, todo, mcp_*) queda fuera.
var readOnlyToolAllowlist = map[string]bool{
	"read":                  true,
	"read_many_files":       true,
	"glob":                  true,
	"grep":                  true,
	"git_status":            true,
	"git_diff":              true,
	"read_skill":            true,
	"web_fetch":             true,
	"web_search":            true,
	"todowrite":             true,
	"update_plan":           true,
	"lsp_diagnostics":       true,
	"lsp_hover":             true,
	"lsp_implementation":    true,
	"lsp_type_definition":   true,
	"lsp_document_symbols":  true,
	"lsp_workspace_symbols": true,
	"lsp_code_action":       true,
	"lsp_completion":        true,
}

// visibleTools filtra las herramientas según el agente.
func (r *Runner) visibleTools(agent domain.Agent) []domain.Tool {
	available := r.tools.ListTools()
	if agent.IsReadOnly {
		// Modo plan: solo herramientas del allowlist de lectura. Nada de
		// escribir, ejecutar, lanzar sub-agentes ni renombrar (lsp_rename).
		readOnly := make([]domain.Tool, 0, len(available))
		for _, tool := range available {
			if readOnlyToolAllowlist[tool.Name] {
				readOnly = append(readOnly, tool)
			}
		}
		return readOnly
	}
	return r.tools.LookupTools(agent.AllowedTools)
}

// buildMessages construye la lista de mensajes para el proveedor (vista compactada).
func (r *Runner) buildMessages(systemPrompt string, session domain.Session) []domain.Message {
	messages := make([]domain.Message, 0, len(session.Messages)+2)
	messages = append(messages, domain.NewTextMessage(domain.RoleSystem, systemPrompt))
	visible := r.compactedVisible(session)
	messages = append(messages, visible...)
	return messages
}

func (r *Runner) compactedVisible(s domain.Session) []domain.Message {
	// Si hay summary+boundary, colapsar.
	if s.CompactBoundary > 0 && s.CompactionSummary != "" {
		boundary := s.CompactBoundary
		if boundary < 0 {
			boundary = 0
		}
		if boundary > len(s.Messages) {
			boundary = len(s.Messages)
		}
		var out []domain.Message
		summary := "Another language model started to solve this task and produced a summary. Use it to build on work already done and avoid duplicating effort.\n\n## Session Summary (compacted)\n" + strings.TrimSpace(s.CompactionSummary)
		out = append(out, domain.Message{
			Role:    domain.RoleSystem,
			Content: []domain.ContentPart{{Type: "text", Text: summary}},
		})
		for i := boundary; i < len(s.Messages); i++ {
			out = append(out, r.projectMessage(s.Messages[i]))
		}
		return out
	}
	var out []domain.Message
	for _, m := range s.Messages {
		out = append(out, r.projectMessage(m))
	}
	return out
}

func (r *Runner) projectMessage(m domain.Message) domain.Message {
	if m.CompactedAt != nil && m.Role == domain.RoleTool {
		return domain.Message{
			Role:       m.Role,
			ToolCallID: m.ToolCallID,
			ToolName:   m.ToolName,
			Content:    []domain.ContentPart{{Type: "text", Text: "[tool result cleared — use read/grep to re-fetch if needed]"}},
			CreatedAt:  m.CreatedAt,
		}
	}
	return m
}

// maybeCompact aplica 2-step compaction: prune (cero LLM) → LLM summary si aún overflow.
// focus permite /compact con instrucciones (Claude Compact Instructions).
func (r *Runner) maybeCompact(ctx context.Context, sess *domain.Session, focus string) error {
	if r.compaction.Disabled {
		return nil
	}
	threshold := r.compaction.Threshold
	if threshold == 0 {
		threshold = 0.85
	}
	// Env override FORGEN_DISABLE_AUTOCOMPACT
	if v := strings.TrimSpace(strings.ToLower(strings.TrimSpace(envOr("", "FORGEN_DISABLE_AUTOCOMPACT")))); v == "1" || v == "true" {
		return nil
	}
	if sess.CompactionCount >= 3 {
		// Anti-thrashing: 3 compactions seguidas sin bajar suficiente → pausar.
		// Solo permitir manual con focus.
		if focus == "" {
			r.logger.Warn("compaction thrashing guard", "session", sess.ID, "count", sess.CompactionCount)
			return nil
		}
	}
	needsOverflow := isOverflowLocal(*sess, sess.Model, r.compaction.ModelMetadata, threshold)
	if !needsOverflow && focus == "" {
		return nil
	}
	// Step 1: prune no-destructivo (siempre, barato).
	pruneable, _ := needsPruneLocal(*sess)
	if pruneable {
		*sess, _ = pruneLocal(*sess)
		if err := r.sessions.Save(ctx, *sess); err != nil {
			return err
		}
		r.messenger.Notice(sess.ID, "Pruned old tool results to free context (no LLM cost)")
		// Re-evaluar overflow tras prune.
		if !isOverflowLocal(*sess, sess.Model, r.compaction.ModelMetadata, threshold) && focus == "" {
			return nil
		}
	}
	// Step 2: LLM summary (5 headings) — requiere provider.
	if r.provider == nil {
		return nil
	}
	// Si thrashing, solo prune ya hecho, no LLM.
	if sess.CompactionCount >= 3 && focus == "" {
		return nil
	}
	summary, err := summarizeLocal(ctx, r.provider, sess.Model, *sess, focus)
	if err != nil {
		return err
	}
	*sess = applyCompactionLocal(*sess, summary)
	if err := r.sessions.Save(ctx, *sess); err != nil {
		return err
	}
	// 7.6.1 memoria auto
	if sess.Workspace != "" {
		memory.New(sess.Workspace).AppendCompaction(summary)
	} else {
		// fallback cwd
		if wd, err := os.Getwd(); err == nil {
			memory.New(wd).AppendCompaction(summary)
		} else {
			memory.New(".").AppendCompaction(summary)
		}
	}
	// Guardar también memoria en .forgen/plans para trazabilidad
	_ = os.MkdirAll(filepath.Join(".forgen", "plans"), 0755)
	r.messenger.Notice(sess.ID, "Compacted session history — summary injected, tail preserved")
	r.logger.Info("compaction.summary", "session", sess.ID, "boundary", sess.CompactBoundary, "chars", len(summary))
	return nil
}

// CompactNow expone compactación manual para CLI /compact.
func (r *Runner) CompactNow(ctx context.Context, sess *domain.Session, focus string) error {
	return r.maybeCompactForced(ctx, sess, focus)
}

func (r *Runner) maybeCompactForced(ctx context.Context, sess *domain.Session, focus string) error {
	pruneable, _ := needsPruneLocal(*sess)
	if pruneable {
		*sess, _ = pruneLocal(*sess)
	}
	if r.provider == nil {
		return r.sessions.Save(ctx, *sess)
	}
	summary, err := summarizeLocal(ctx, r.provider, sess.Model, *sess, focus)
	if err != nil {
		return err
	}
	*sess = applyCompactionLocal(*sess, summary)
	if sess.Workspace != "" {
		memory.New(sess.Workspace).AppendCompaction(summary)
	}
	if err := r.sessions.Save(ctx, *sess); err != nil {
		return err
	}
	r.messenger.Notice(sess.ID, "Compacted (manual) — summary injected")
	return nil
}

// Helpers locales sin importar session/compaction cycle (Runner está en otro paquete).
func isOverflowLocal(s domain.Session, model domain.Model, md map[string]domain.ModelMetadata, threshold float64) bool {
	limit := 128000
	if m, ok := md[model.Key()]; ok && m.ContextLimit > 0 {
		limit = m.ContextLimit
	}
	reserved := 4096
	if m, ok := md[model.Key()]; ok && m.MaxOutput > 0 {
		reserved = m.MaxOutput
	}
	usable := limit - reserved
	if usable <= 0 {
		usable = limit
	}
	budget := int(float64(usable) * threshold)
	tokens := 0
	for _, msg := range s.Messages {
		tokens += 4
		for _, p := range msg.Content {
			tokens += len(p.Text)/4 + 1
			if p.Call != nil {
				tokens += len(p.Call.Name)/4 + 1
			}
		}
	}
	return tokens >= budget
}

func needsPruneLocal(s domain.Session) (bool, int) {
	pruneable := 0
	protected := protectedLocal(s)
	for i, m := range s.Messages {
		if protected[i] {
			continue
		}
		if m.Role == domain.RoleTool && m.CompactedAt == nil {
			pruneable += len(m.Text())/4 + 1
		}
	}
	return pruneable >= 20000, pruneable
}

func protectedLocal(s domain.Session) map[int]bool {
	protected := make(map[int]bool)
	acc := 0
	for i := len(s.Messages) - 1; i >= 0; i-- {
		m := s.Messages[i]
		if m.Role == domain.RoleTool {
			if acc < 40000 {
				protected[i] = true
				acc += len(m.Text())/4 + 1
			}
		}
	}
	userTurns := 0
	for i := len(s.Messages) - 1; i >= 0 && userTurns < 2; i-- {
		if s.Messages[i].Role == domain.RoleUser {
			protected[i] = true
			userTurns++
			if i+1 < len(s.Messages) {
				protected[i+1] = true
			}
		}
	}
	for i, m := range s.Messages {
		if m.ToolName == "read_skill" {
			protected[i] = true
		}
	}
	return protected
}

func pruneLocal(s domain.Session) (domain.Session, int) {
	protected := protectedLocal(s)
	now := time.Now()
	marked := 0
	for i := range s.Messages {
		if protected[i] {
			continue
		}
		if s.Messages[i].Role == domain.RoleTool && s.Messages[i].CompactedAt == nil {
			t := now
			s.Messages[i].CompactedAt = &t
			marked++
		}
	}
	if marked > 0 {
		s.CompactionCount++
	}
	return s, marked
}

func summarizeLocal(ctx context.Context, provider ports.LLMProvider, model domain.Model, s domain.Session, focus string) (string, error) {
	lang := "en"
	for _, m := range s.Messages {
		if m.Role == domain.RoleUser {
			t := strings.ToLower(m.Text())
			if strings.Contains(t, "añad") || strings.Contains(t, "crea") || strings.Contains(t, "página") || strings.Contains(t, "implementa") {
				lang = "es"
			}
			break
		}
	}
	var sys, user string
	if lang == "es" {
		sys = "Eres un asistente que resume conversaciones para continuar la sesión. Genera un resumen detallado pero conciso. Esta será la ÚNICA memoria disponible al continuar, así que preserva: qué se hizo, en qué se trabaja, archivos modificados y estado, qué falta por hacer, peticiones/restricciones clave y decisiones técnicas con porqué. Sé conciso pero suficiente."
		user = "Resume nuestra conversación anterior. Este resumen será el único contexto al continuar, así que preserva: qué se logró, trabajo en progreso, archivos involucrados, próximos pasos y peticiones/restricciones clave."
	} else {
		sys = "You are an assistant that summarizes conversations to continue the session. Generate a detailed but concise summary. This will be the ONLY memory when continuing, so preserve: what was done, what is in progress, files modified and status, what remains, key requests/constraints and decisions with rationale. Be concise but sufficient."
		user = "Summarize our conversation above. This summary will be the only context when continuing, so preserve: what was accomplished, work in progress, files involved, next steps and key requests/constraints."
	}
	if strings.TrimSpace(focus) != "" {
		if lang == "es" {
			user += "\n\nEnfoque solicitado: " + focus
		} else {
			user += "\n\nFocus requested: " + focus
		}
	}
	// Construir msgs visibles
	var msgs []domain.Message
	msgs = append(msgs, domain.NewTextMessage(domain.RoleSystem, sys))
	// Visible projection
	for _, m := range s.Messages {
		proj := m
		if m.CompactedAt != nil && m.Role == domain.RoleTool {
			proj = domain.Message{Role: m.Role, ToolCallID: m.ToolCallID, ToolName: m.ToolName, Content: []domain.ContentPart{{Type: "text", Text: "[tool result cleared]"}}}
		}
		msgs = append(msgs, proj)
	}
	msgs = append(msgs, domain.NewTextMessage(domain.RoleUser, user))
	var summary strings.Builder
	req := ports.ChatRequest{Model: model, Messages: msgs, Temperature: 0.2, MaxTokens: 2048}
	err := provider.StreamChat(ctx, req, func(ev ports.StreamEvent) error {
		if d, ok := ev.(ports.TextDeltaEvent); ok {
			summary.WriteString(d.Text)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(summary.String())
	if out == "" {
		return "", fmt.Errorf("compaction: resumen vacío")
	}
	return out, nil
}

func applyCompactionLocal(s domain.Session, summary string) domain.Session {
	tail := 20
	if len(s.Messages) < tail {
		tail = len(s.Messages)
	}
	s.CompactBoundary = len(s.Messages) - tail
	if s.CompactBoundary < 0 {
		s.CompactBoundary = 0
	}
	s.CompactionSummary = strings.TrimSpace(summary)
	s.CompactionCount++
	return s
}

func envOr(def, key string) string {
	// wrapper para testabilidad sin os.Getenv directo
	_ = def
	// lazy import avoid cycle: usar os.Getenv via string key
	return strings.TrimSpace(getEnv(key))
}

// getEnv es sobreescribible en tests.
var getEnv = func(key string) string {
	// import os lazily to avoid import loop in header
	return osGetenv(key)
}

// osGetenv via indirection to allow mock
func osGetenv(key string) string {
	return os.Getenv(key)
}

func extractPatchPath(patch string) string {
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "*** Update File:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "*** Update File:"))
		}
		if strings.HasPrefix(line, "*** Add File:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "*** Add File:"))
		}
		if strings.HasPrefix(line, "+++ b/") {
			return strings.TrimSpace(strings.TrimPrefix(line, "+++ b/"))
		}
	}
	return ""
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
