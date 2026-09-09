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

// fewShotForSmallModels son ejemplos de tool calling para modelos pequeños
// que tienden a "hablar" en lugar de ejecutar herramientas.
var fewShotForSmallModels = []domain.Message{
	domain.NewTextMessage(domain.RoleUser, "¿Cuántos archivos .go hay en src/?"),
	domain.NewTextMessage(domain.RoleAssistant, "Para contar archivos .go necesito buscarlos. Usaré la herramienta glob con patrón src/**/*.go."),
	domain.NewTextMessage(domain.RoleTool, "Hay 12 archivos .go en src/: file1.go, file2.go..."),
	domain.NewTextMessage(domain.RoleAssistant, "Hay 12 archivos .go en src/."),
	domain.NewTextMessage(domain.RoleUser, "Muestra el contenido de config.yaml"),
	domain.NewTextMessage(domain.RoleAssistant, "Para mostrar el contenido necesito leer el archivo. Usaré read con path config.yaml."),
	domain.NewTextMessage(domain.RoleTool, "Contenido: ..."),
	domain.NewTextMessage(domain.RoleAssistant, "El contenido de config.yaml es: ..."),
	// Ejemplo crítico para el bug de directorio vs archivo (Gemma-4B)
	domain.NewTextMessage(domain.RoleUser, "Genera un html autocontenido en Test-local"),
	domain.NewTextMessage(domain.RoleAssistant, "Voy a crear el archivo HTML. Usaré write con path Test-local/index.html (debe incluir nombre de archivo, no solo directorio)."),
	domain.NewTextMessage(domain.RoleTool, "Archivo Test-local/index.html escrito (1024 bytes)"),
	domain.NewTextMessage(domain.RoleAssistant, "Listo, creado Test-local/index.html."),
}

// samplingForTier devuelve temperatura, top_p y top_k según el tier del modelo.
// small/light (≤9b): temp 0.0, top_p 0.95, top_k 40 — determinista para tool-calling fiable.
// standard: temp 0.2, top_p 0.95 — balanceado.
// heavy: temp 0.2, top_p 0.95 — creatividad para tareas complejas.
func samplingForTier(tier domain.Tier) (temp float64, topP *float64, topK *int) {
	switch tier {
	case domain.TierLight:
		t := 0.0
		p := 0.95
		k := 40 // top_k 40-50 recomendado para 7-9B en tool calling
		return t, &p, &k
	case domain.TierHeavy:
		t := 0.2
		p := 0.95
		return t, &p, nil
	default: // TierStandard
		t := 0.2
		p := 0.95
		return t, &p, nil
	}
}

// maxTokensForTier devuelve el máximo de tokens según tier y metadata del modelo.
// Light: 2048 · Standard: 4096 · Heavy: 8192 (subidos desde 1024/1024/4096:
// con 1024 un apply_patch multi-archivo se truncaba por `length` y el turno se perdía).
// Si ModelMetadata.MaxOutput está definido y es > 0, se usa ese valor.
func maxTokensForTier(tier domain.Tier, meta domain.ModelMetadata) int {
	if meta.MaxOutput > 0 {
		return meta.MaxOutput
	}
	switch tier {
	case domain.TierLight:
		return 2048
	case domain.TierHeavy:
		return 8192
	default: // TierStandard
		return 4096
	}
}

// inferTierFromID deduce el tier del modelo por su ID usando la heurística centralizada.
func inferTierFromID(id string) domain.Tier {
	return domain.InferTierFromID(id)
}

// shouldRetrySmallModel detecta respuestas evasivas de modelos pequeños.
func shouldRetrySmallModel(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return true
	}
	// Patrones de evasión comunes en modelos pequeños
	evasivePatterns := []string{
		"no puedo", "no tengo acceso", "no estoy seguro", "no sé",
		"como modelo de lenguaje", "como ia", "limitar mi capacidad",
		"no dispongo de", "no tengo información",
		"dime la ruta exacta", "especifica el nombre del archivo",
	}
	for _, pat := range evasivePatterns {
		if strings.Contains(text, pat) {
			return true
		}
	}
	// Si la respuesta es muy corta y no contiene tool calls, probar de nuevo
	if len(text) < 50 {
		return true
	}
	return false
}

// shouldRetryToolError detecta tool results con errores típicos de SLM (is a directory, old_string no encontrado)
func shouldRetryToolError(toolMessages []domain.Message) bool {
	for _, m := range toolMessages {
		if m.Role != domain.RoleTool {
			continue
		}
		txt := strings.ToLower(m.Text())
		if strings.Contains(txt, "is a directory") || strings.Contains(txt, "es un directorio") {
			return true
		}
	}
	return false
}

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
	// Medición real con system+tools para no compactar tarde (alucinaciones).
	if err := r.maybeCompactWithContext(ctx, &input.Session, "", systemPrompt, tools); err != nil {
		r.logger.Warn("auto-compact pre-turn", "err", err)
	}

	totalToolCalls := 0
	for iteration := 0; iteration < r.maxIterations; iteration++ {
		messages := r.buildMessages(systemPrompt, input.Session)

		// Guard pre-LLM: si el request ya supera el 95% (system+tools+msgs),
		// compactar y reconstruir antes de llamar al provider (evita prompt_too_long
		// a mitad de un turno largo de 50 iteraciones).
		if session.UsageRatio(input.Session, systemPrompt, tools, input.Session.Model, r.compaction.ModelMetadata) >= session.ForceThreshold {
			r.logger.Warn("auto-compact pre-llm forced", "session", input.Session.ID, "iteration", iteration)
			if err := r.maybeCompactWithContext(ctx, &input.Session, "", systemPrompt, tools); err != nil {
				r.logger.Warn("auto-compact pre-llm", "err", err)
			}
			messages = r.buildMessages(systemPrompt, input.Session)
		}

		// Emitir eventos de observabilidad.
		r.logger.Info("llm.request", "session", input.Session.ID, "iteration", iteration,
			"model", input.Session.Model.Key(), "messages", len(messages), "tools", len(tools))

		response, err := r.callLLM(ctx, input.Session.ID, input.Session.Model, messages, tools, input.Phase, iteration)
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

		// Plan gate (claude-code strict): sesión con plan pendiente → build no
		// ejecuta mutaciones hasta aprobar (exit_plan_mode o /approve).
		if gated := r.applyPlanGate(input.Session, input.Agent, response.toolCalls); gated != nil {
			assistantMessage := domain.NewAssistantWithToolCalls(response.text, response.toolCalls)
			input.Session.Messages = append(input.Session.Messages, assistantMessage)
			input.Session.Messages = append(input.Session.Messages, gated...)
			if err := r.sessions.Save(ctx, input.Session); err != nil {
				return RunResult{}, err
			}
			continue
		}

		// Ejecutar herramientas y recoger resultados.
		assistantMessage := domain.NewAssistantWithToolCalls(response.text, response.toolCalls)
		toolMessages, err := r.executeTools(ctx, input.Session.ID, input.Workspace, input.Agent, response.toolCalls)
		if err != nil {
			return RunResult{}, err
		}
		input.Session.Messages = append(input.Session.Messages, assistantMessage)
		input.Session.Messages = append(input.Session.Messages, toolMessages...)
		// Plan status: plan artifact escrito → pending; exit_plan_mode OK → approved.
		r.updatePlanStatus(&input.Session, input.Agent, response.toolCalls, toolMessages)
		// Escalado mid-loop: 3 errores consecutivos → guía correctiva inyectada.
		if streak := consecutiveToolErrors(toolMessages); streak >= 3 {
			guide := domain.NewTextMessage(domain.RoleUser, "GUÍA AUTOMÁTICA: llevas 3+ errores de herramientas seguidos. Detente, re-lee los outputs con `read`/`git_diff`, formula una hipótesis distinta y pide confirmación con `ask_question` si sigues atascado. No repitas la misma llamada.")
			input.Session.Messages = append(input.Session.Messages, guide)
			r.messenger.Notice(input.Session.ID, "⚠ 3 errores seguidos — inyectada guía correctiva para romper el ciclo.")
			r.logger.Warn("tool.error_streak", "session", input.Session.ID, "streak", streak)
		} else {
			recordToolErrorOutcome(true, "")
		}
		if err := r.sessions.Save(ctx, input.Session); err != nil {
			return RunResult{}, err
		}
		// Auto-compaction tras tool results si superamos umbral (no bloquea si falla).
		// Con medición real system+tools.
		if err := r.maybeCompactWithContext(ctx, &input.Session, "", systemPrompt, tools); err != nil {
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
	reason        domain.FinishReason
}

// llmTimeout acota cada llamada al proveedor para que una API colgada no
// deje el spinner girando indefinidamente ni bloquee la cancelación.
const llmTimeout = 150 * time.Second

func (r *Runner) callLLM(ctx context.Context, sessionID string, model domain.Model,
	messages []domain.Message, tools []domain.Tool, phase domain.AgentPhase, iteration int) (llmResponse, error) {
	var response llmResponse
	var mutex sync.Mutex
	var builder strings.Builder
	builder.Grow(8192)
	// Token budget & cost guard relativo al modelo (antes fijo >90000: un 32k
	// reventaba mucho antes sin avisar). Mide system+tools+msgs reales.
	budgetTokens := session.SessionTokens(domain.Session{Messages: messages}) + session.ToolsOverhead(tools)
	usable, _ := session.UsableBudget(model, r.compaction.ModelMetadata, 1.0)
	if usable > 0 && float64(budgetTokens) >= float64(usable)*session.WarnThreshold {
		r.logger.Warn("token.budget_high", "session", sessionID, "tokens", budgetTokens, "usable", usable, "hint", "consider /compact or fresh session")
		r.messenger.Notice(sessionID, fmt.Sprintf("⚠ Tokens altos: %d/%d — considera /compact o sesión nueva para ahorrar coste", budgetTokens, usable))
	}

	ctx, cancel := context.WithTimeout(ctx, llmTimeout)
	defer cancel()

	// Inferir tier del modelo para sampling adaptativo.
	// Si model.Tier está vacío, usar inferencia por nombre (misma lógica que orchestrator).
	modelTier := domain.Tier(model.Tier)
	if modelTier == "" {
		modelTier = inferTierFromID(model.ID)
	}

	// Log tiered para debugging
	r.logger.Info("llm.call", "session", sessionID, "model", model.ID, "provider", model.Provider, "tier", string(modelTier), "phase", string(phase), "tools", len(tools), "messages", len(messages))

	// Log extra para modelos pequeños
	if modelTier == domain.TierLight {
		r.logger.Debug("llm.small_model_optimizations", "session", sessionID, "model", model.ID, "sampling", "temp=0.0 top_p=0.95 top_k=40", "few_shot", phase == domain.PhasePlan)
	}

	temp, topP, topK := samplingForTier(modelTier)

	// Filtrar TopK solo para proveedores que lo soportan (OpenAI-compatible locales)
	// OpenAI y Anthropic oficiales no aceptan top_k
	if model.Provider == "openai" || model.Provider == "anthropic" {
		topK = nil
	}

	// Para modelos pequeños, inyectar few-shot examples de tool calling
	// para mejorar la tasa de aciertos. Solo en iteración 0 para limitar tokens.
	messagesWithExamples := messages
	if modelTier == domain.TierLight && len(tools) > 0 && iteration == 0 {
		messagesWithExamples = append(fewShotForSmallModels, messages...)
	}

	request := ports.ChatRequest{
		Model:           model,
		Messages:        messagesWithExamples,
		Tools:           tools,
		Temperature:     temp,
		TopP:            topP,
		TopK:            topK,
		MaxTokens:       maxTokensForTier(modelTier, r.compaction.ModelMetadata[model.ID]),
		ReasoningEffort: r.reasoningEffort,
	}

	err := r.provider.StreamChat(ctx, request, func(event ports.StreamEvent) error {
		switch typedEvent := event.(type) {
		case ports.TextDeltaEvent:
			mutex.Lock()
			builder.WriteString(typedEvent.Text)
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
			mutex.Lock()
			response.reason = typedEvent.Reason
			mutex.Unlock()
			r.logger.Info("llm.response", "session", sessionID, "reason", typedEvent.Reason)
		case ports.ErrorEvent:
			r.logger.Error("llm.stream_error", "session", sessionID, "err", typedEvent.Err)
		}
		return nil
	})
	if err != nil {
		return llmResponse{}, fmt.Errorf("llm %s/%s: %w", model.Provider, model.ID, err)
	}
	mutex.Lock()
	response.text = builder.String()
	mutex.Unlock()

	// Continuación automática si el modelo se quedó sin max_tokens a mitad de un
	// tool call / patch. Sin esto, un patch grande se trunca y el turno se pierde.
	if response.reason == domain.FinishReasonMaxTokens && !response.hasToolCalls {
		r.logger.Info("llm.continue_on_length", "session", sessionID, "model", model.ID)
		contMsgs := append(append([]domain.Message{}, messagesWithExamples...),
			domain.NewTextMessage(domain.RoleAssistant, response.text),
			domain.NewTextMessage(domain.RoleUser, "Continúa exactamente donde te quedaste. Completa el tool call pendiente sin repetir lo ya dicho."),
		)
		contReq := ports.ChatRequest{
			Model:           model,
			Messages:        contMsgs,
			Tools:           tools,
			Temperature:     temp,
			TopP:            topP,
			TopK:            topK,
			MaxTokens:       maxTokensForTier(modelTier, r.compaction.ModelMetadata[model.ID]),
			ReasoningEffort: r.reasoningEffort,
		}
		var contBuilder strings.Builder
		contResp := llmResponse{}
		if cerr := r.provider.StreamChat(ctx, contReq, func(event ports.StreamEvent) error {
			switch te := event.(type) {
			case ports.TextDeltaEvent:
				contBuilder.WriteString(te.Text)
				r.messenger.StreamText(sessionID, te.Text)
			case ports.ToolCallEvent:
				contResp.toolCalls = append(contResp.toolCalls, te.Call)
				contResp.toolCallCount++
				contResp.hasToolCalls = true
			case ports.UsageEvent:
				contResp.usage = te.Usage
			case ports.DoneEvent:
				contResp.reason = te.Reason
			}
			return nil
		}); cerr == nil && (contResp.hasToolCalls || strings.TrimSpace(contBuilder.String()) != "") {
			if contResp.hasToolCalls {
				response.toolCalls = append(response.toolCalls, contResp.toolCalls...)
				response.toolCallCount += contResp.toolCallCount
				response.hasToolCalls = true
				response.reason = contResp.reason
			}
			response.text += contBuilder.String()
			response.usage.OutputTokens += contResp.usage.OutputTokens
			response.usage.InputTokens += contResp.usage.InputTokens
		}
	}

	// Retry universal para respuestas evasivas o sin tool calls cuando se esperaban.
	// Antes solo corría para providers locales y TierLight: los modelos grandes
	// (30B/Opus vía API externa) también fallan turnos y no tenían 2ª oportunidad.
	// Solo en iteración 0: una respuesta corta tras ejecutar herramientas es una
	// respuesta final legítima ("listo"), no un fallo de tool-calling.
	if !response.hasToolCalls && len(tools) > 0 && iteration == 0 {
		if shouldRetrySmallModel(response.text) {
			isLocal := model.Provider != "openai" && model.Provider != "anthropic"
			// Locales: siempre reintentar. Remotos: reintentar solo si la respuesta
			// es claramente evasiva/vacía (evita duplicar coste en respuestas finales legítimas).
			evasive := isEvasiveResponse(response.text)
			if isLocal || evasive || response.reason == domain.FinishReasonMaxTokens {
				r.logger.Info("llm.retry_no_toolcalls", "session", sessionID, "model", model.ID, "local", isLocal)
				retrySystem := "IMPORTANTE: Tu respuesta anterior no usó herramientas o fue evasiva. Debes usar herramientas cuando el usuario pida información sobre archivos, código o estado del proyecto. NO inventes respuestas. Si no puedes responder, ejecuta la herramienta adecuada."
				messagesWithRetry := append([]domain.Message{
					domain.NewTextMessage(domain.RoleSystem, retrySystem),
				}, messagesWithExamples...)
			
			// Limpiar respuesta previa para re-stream
			builder.Reset()
			response = llmResponse{}
			
			topP := 0.95
			topK := 40
			// Filtrar TopK solo para proveedores que lo soportan
			retryTopK := &topK
			if model.Provider == "openai" || model.Provider == "anthropic" {
				retryTopK = nil
			}
requestRetry := ports.ChatRequest{
			Model:           model,
			Messages:        messagesWithRetry,
			Tools:           tools,
			Temperature:     0.0,
			TopP:            &topP,
			TopK:            retryTopK,
			MaxTokens:       maxTokensForTier(modelTier, r.compaction.ModelMetadata[model.ID]),
			ReasoningEffort: r.reasoningEffort,
		}
			
			err = r.provider.StreamChat(ctx, requestRetry, func(event ports.StreamEvent) error {
				switch typedEvent := event.(type) {
				case ports.TextDeltaEvent:
					mutex.Lock()
					builder.WriteString(typedEvent.Text)
					mutex.Unlock()
					// No re-stream text para evitar duplicados; ya se mostró el primer intento
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
				}
				return nil
			})
			if err == nil {
				mutex.Lock()
				response.text = builder.String()
				mutex.Unlock()
			}
			}
		}
	}

	if response.toolCallCount > 0 && len(response.toolCalls) != response.toolCallCount {
		return llmResponse{}, errors.New("llm: llamadas a herramientas incompletas en el streaming")
	}
	r.recordUsage(ctx, sessionID, model, phase, response.usage)
	return response, nil
}

// isEvasiveResponse distingue una respuesta final legítima (texto largo y útil)
// de una evasiva/vacía que merece retry incluso en providers remotos de pago.
func isEvasiveResponse(text string) bool {
	t := strings.ToLower(strings.TrimSpace(text))
	if t == "" || len(t) < 50 {
		return true
	}
	for _, pat := range []string{
		"no puedo", "no tengo acceso", "no estoy seguro", "no sé",
		"como modelo de lenguaje", "como ia", "no dispongo de", "no tengo información",
	} {
		if strings.Contains(t, pat) {
			return true
		}
	}
	return false
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
		// Check readOnly block primero (sync, sin I/O) — con excepción para plan artifact
		if agent.IsReadOnly && !readOnlyToolAllowlist[call.Name] {
			// Permitir write solo a .forgen/plans/* en modo plan (claude-code strict)
			if call.Name == "write" {
				if p, ok := call.Arguments["path"].(string); ok && isPlanArtifactPath(p) {
					// permitir, no denegar
				} else {
					denied := domain.ToolResult{
						ToolCallID: call.ID,
						OK:         false,
						Output:     deniedResultMessage,
						Error:      fmt.Errorf("%s: herramienta %q no permitida en modo plan (solo .forgen/plans/* permitido)", deniedResultMessage, call.Name),
					}
					permResults[i] = permResult{idx: i, call: call, result: &denied}
					continue
				}
			} else {
				denied := domain.ToolResult{
					ToolCallID: call.ID,
					OK:         false,
					Output:     deniedResultMessage,
					Error:      fmt.Errorf("%s: herramienta %q no permitida en modo plan", deniedResultMessage, call.Name),
				}
				permResults[i] = permResult{idx: i, call: call, result: &denied}
				continue
			}
		}
		decision, err := r.decider.Decide(ctx, sessionID, call)
		if err != nil {
			denied := domain.ToolResult{ToolCallID: call.ID, OK: false, Error: fmt.Errorf("decidir permiso: %w", err)}
			permResults[i] = permResult{idx: i, call: call, result: &denied}
			continue
		}
		if !decision.Allowed && decision.Level == domain.PermissionOnRequest {
			choice, confirmErr := r.responder.Confirm(ctx, sessionID, call)
			if confirmErr != nil {
				denied := domain.ToolResult{ToolCallID: call.ID, OK: false, Error: confirmErr}
				permResults[i] = permResult{idx: i, call: call, result: &denied}
				continue
			}
			if choice.Allowed {
				decision = domain.Decision{Allowed: true, Level: domain.PermissionOnRequest, Reason: "confirmado por usuario"}
				if choice.Remember {
					r.rememberAllow(call)
				}
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

	// Ejecutar: lecturas en paralelo (límite 5), escrituras en secuencia.
	// Las escrituras al mismo archivo en paralelo causaban races read-after-edit.
	toolMessages := make([]domain.Message, len(calls))
	execOne := func(idx int, call domain.ToolCall) {
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
	}
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
		if isWriteTool(pr.call.Name) {
			continue // fase secuencial abajo
		}
		wg.Add(1)
		go func(idx int, call domain.ToolCall) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			execOne(idx, call)
		}(i, pr.call)
	}
	wg.Wait()
	// Fase secuencial de escrituras en orden de llamada (respeta dependencias).
	for i, pr := range permResults {
		if pr.result != nil || !isWriteTool(pr.call.Name) {
			continue
		}
		execOne(i, pr.call)
	}
	return toolMessages, nil
}

// isWriteTool indica herramientas con efectos de escritura/ejecución.
func isWriteTool(name string) bool {
	switch name {
	case "write", "edit", "apply_patch", "bash", "todo", "todowrite", "update_plan":
		return true
	default:
		return false
	}
}

// doom-loop: 3× mismo tool+args exactos + 3× mismo archivo + errores consecutivos.
var doomHistory = struct {
	sync.Mutex
	recent     []string
	errStreak  int
	lastErrKey string
}{}

func toolTargetPath(c domain.ToolCall) string {
	for _, k := range []string{"path", "file", "patch"} {
		if v, ok := c.Arguments[k].(string); ok && v != "" {
			if k == "patch" {
				if p := extractPatchPath(v); p != "" {
					return p
				}
				continue
			}
			return v
		}
	}
	return ""
}

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
		// Detección semántica: mismo tool + mismo archivo 3x aunque cambien otros args.
		if target := toolTargetPath(c); target != "" {
			semKey := c.Name + "@" + target
			semCount := 0
			for _, rk := range doomHistory.recent {
				// rk es name:args; aproximamos buscando el target dentro.
				if strings.Contains(rk, target) && strings.HasPrefix(rk, c.Name+":") {
					semCount++
				}
			}
			_ = semKey
			if semCount >= 3 {
				return fmt.Sprintf("⚠ Loop semántico: '%s' sobre '%s' %dx seguidas con el mismo resultado — re-lee el archivo con `read`, revisa `git_diff` y cambia de estrategia antes de reintentar.", c.Name, target, semCount)
			}
		}
	}
	return ""
}

// planMutatingTools son las herramientas que el gate plan→build bloquea
// cuando hay un plan pendiente de aprobación.
var planMutatingTools = map[string]bool{
	"write": true, "edit": true, "apply_patch": true, "bash": true, "lsp_rename": true,
}

// applyPlanGate deniega mutaciones en build si la sesión tiene plan pendiente.
// Devuelve nil si no aplica (sin plan, ya aprobado, o agente read-only).
func (r *Runner) applyPlanGate(sess domain.Session, agent domain.Agent, calls []domain.ToolCall) []domain.Message {
	if agent.IsReadOnly || sess.PlanStatus != "pending" {
		return nil
	}
	denied := false
	for _, c := range calls {
		if planMutatingTools[c.Name] {
			denied = true
			break
		}
	}
	if !denied {
		return nil
	}
	r.messenger.Notice(sess.ID, "Plan pendiente de aprobación — mutaciones bloqueadas hasta aprobar (exit_plan_mode o /approve).")
	r.logger.Warn("plan.gate_deny", "session", sess.ID)
	out := make([]domain.Message, 0, len(calls))
	for _, c := range calls {
		text := ""
		if planMutatingTools[c.Name] {
			text = "PLAN GATE: hay un plan pendiente de aprobación en esta sesión. No ejecuto mutaciones hasta que apruebes: vuelve a plan y usa exit_plan_mode, o ejecuta /approve en la TUI."
		} else {
			text = "PLAN GATE: herramienta de lectura permitida (el gate solo bloquea mutaciones)."
		}
		out = append(out, domain.NewToolResultMessage(c.ID, c.Name,
			domain.ToolResult{ToolCallID: c.ID, OK: !planMutatingTools[c.Name], Output: text}))
	}
	return out
}

// updatePlanStatus mantiene el gate: artefacto de plan escrito → pending
// (re-planear revoca la aprobación); exit_plan_mode OK → approved.
func (r *Runner) updatePlanStatus(sess *domain.Session, agent domain.Agent, calls []domain.ToolCall, results []domain.Message) {
	if !agent.IsReadOnly {
		return
	}
	changed := false
	for i, c := range calls {
		var ok bool
		if i < len(results) {
			ok = !strings.HasPrefix(results[i].Text(), "ERROR:")
		}
		if !ok {
			continue
		}
		if c.Name == "write" {
			if p, _ := c.Arguments["path"].(string); p != "" && isPlanArtifactPath(p) {
				if sess.PlanStatus != "pending" {
					sess.PlanStatus = "pending"
					changed = true
				}
			}
		}
		if c.Name == "exit_plan_mode" {
			sess.PlanStatus = "approved"
			if plan, _ := c.Arguments["plan"].(string); plan != "" {
				if len(plan) > 2000 {
					plan = plan[:2000] + "…"
				}
				sess.PlanSummary = plan
			}
			changed = true
		}
	}
	if changed {
		r.logger.Info("plan.status", "session", sess.ID, "status", sess.PlanStatus)
	}
}

// consecutiveToolErrors cuenta errores al final del batch y actualiza el streak global.
// Además integra shouldRetryToolError: errores típicos de SLM (is a directory)
// alimentan el streak aunque el último mensaje no parezca error.
func consecutiveToolErrors(msgs []domain.Message) int {
	if len(msgs) == 0 {
		recordToolErrorOutcome(true, "")
		return 0
	}
	if shouldRetryToolError(msgs) {
		return recordToolErrorOutcome(false, "slm-tool-arg")
	}
	// Si el último mensaje es OK, se resetea el streak.
	last := msgs[len(msgs)-1].Text()
	lastLower := strings.ToLower(last)
	failed := strings.Contains(lastLower, "error") || strings.Contains(lastLower, "deneg") ||
		strings.Contains(lastLower, "not found") || strings.Contains(lastLower, "no existe") ||
		strings.Contains(lastLower, "is a directory") || strings.Contains(lastLower, "permiso denegado")
	if !failed {
		recordToolErrorOutcome(true, "")
		return 0
	}
	return recordToolErrorOutcome(false, lastLower[:minInt(len(lastLower), 120)])
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// recordToolErrorOutcome alimenta el contador de errores consecutivos para
// escalado mid-loop (tras 3 fallos seguidos se inyecta guía correctiva).
func recordToolErrorOutcome(ok bool, errKey string) int {
	doomHistory.Lock()
	defer doomHistory.Unlock()
	if ok {
		doomHistory.errStreak = 0
		doomHistory.lastErrKey = ""
		return 0
	}
	if errKey != "" && errKey == doomHistory.lastErrKey {
		doomHistory.errStreak++
	} else {
		doomHistory.errStreak = 1
		doomHistory.lastErrKey = errKey
	}
	return doomHistory.errStreak
}

//nolint:unused // retained for direct-call tests and single-tool path
func (r *Runner) executeWithPermission(ctx context.Context, sessionID string, agent domain.Agent, call domain.ToolCall) domain.ToolResult {
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
		choice, confirmErr := r.responder.Confirm(ctx, sessionID, call)
		if confirmErr != nil {
			return domain.ToolResult{ToolCallID: call.ID, OK: false, Error: confirmErr}
		}
		if choice.Allowed {
			decision = domain.Decision{Allowed: true, Level: domain.PermissionOnRequest, Reason: "confirmado por usuario"}
			if choice.Remember {
				r.rememberAllow(call)
			}
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

// keep executeWithPermission used via inline path above; suppress unused linter if inlined.
//nolint:unused // retained for direct call tests and future plan-mode single-tool path

// readOnlyToolAllowlist es el conjunto de herramientas permitidas a los
// agentes de solo lectura (modo plan): solo lectura/exploración e investigación
// (logs, búsqueda en la web, git status/diff y LSP de lectura). Cualquier
// herramienta capaz de modificar archivos, estado o lanzar sub-agentes
// (task, write, edit, bash, apply_patch, lsp_rename, todo, mcp_*) queda fuera.
// Excepción: write solo a .forgen/plans/* se permite vía isPlanArtifactPath (claude-code strict).
var readOnlyToolAllowlist = map[string]bool{
	"read":                  true,
	"read_many_files":       true,
	"ls":                    true,
	"glob":                  true,
	"grep":                  true,
	"git_status":            true,
	"git_diff":              true,
	"read_skill":            true,
	"web_fetch":             true,
	"web_search":            true,
	"todowrite":             true,
	"update_plan":           true,
	"ask_question":          true,
	"exit_plan_mode":        true,
	"lsp_diagnostics":       true,
	"lsp_hover":             true,
	"lsp_implementation":    true,
	"lsp_type_definition":   true,
	"lsp_document_symbols":  true,
	"lsp_workspace_symbols": true,
	"lsp_code_action":       true,
	"lsp_completion":        true,
}

// isPlanArtifactPath permite write en plan mode solo a artefactos de plan (claude-code strict).
// Normaliza `\`→`/` ANTES de Clean: en Windows Clean devuelve `\` y ToSlash
// solo convierte en Windows, así que sin esto el gate fallaba en CI windows
// (y el modo plan denegaba hasta el propio plan en esa plataforma).
func isPlanArtifactPath(p string) bool {
	clean := filepath.ToSlash(filepath.Clean(strings.ReplaceAll(p, "\\", "/")))
	return clean == ".forgen/plans/plan.md" || strings.HasPrefix(clean, ".forgen/plans/")
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
	return session.VisibleMessages(s)
}

// maybeCompactWithContext es la compactación automática con medición real:
// systemPrompt + tools incluidos en el cálculo (antes solo mensajes → compactaba
// tarde y el provider truncaba por detrás, causando alucinaciones).
// Niveles: 70% aviso · 85% prune auto · 95% prune+summary forzado (ignora thrashing).
func (r *Runner) maybeCompactWithContext(ctx context.Context, sess *domain.Session, focus string, systemPrompt string, tools []domain.Tool) error {
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
	md := r.compaction.ModelMetadata
	ratio := session.UsageRatio(*sess, systemPrompt, tools, sess.Model, md)
	forced := focus != ""
	// Nivel 95%: forzar aunque haya thrashing (nunca dejar sin red).
	if !forced && ratio >= session.ForceThreshold {
		forced = true
		r.logger.Warn("compaction.force_95", "session", sess.ID, "ratio", ratio)
	}
	if !forced && sess.CompactionCount >= 3 {
		// Anti-thrashing: 3 compactions seguidas sin bajar suficiente → pausar nivel 85.
		// Solo permitir manual con focus o nivel 95% forzado.
		if focus == "" {
			r.logger.Warn("compaction thrashing guard", "session", sess.ID, "count", sess.CompactionCount, "ratio", ratio)
			return nil
		}
	}
	// Nivel 70%: aviso sin actuar.
	if !forced && ratio >= session.WarnThreshold && !session.IsOverflowTotal(*sess, systemPrompt, tools, sess.Model, md, threshold) {
		r.messenger.Notice(sess.ID, fmt.Sprintf("Contexto al %d%% del usable — considera /compact o sesión nueva para evitar alucinaciones.", int(ratio*100)))
		r.logger.Info("compaction.warn_70", "session", sess.ID, "ratio", ratio)
		return nil
	}
	needsOverflow := session.IsOverflowTotal(*sess, systemPrompt, tools, sess.Model, md, threshold)
	if !needsOverflow && !forced {
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
		if !session.IsOverflowTotal(*sess, systemPrompt, tools, sess.Model, md, threshold) && !forced {
			r.decayCompactionCount(sess, systemPrompt, tools)
			return nil
		}
	}
	// Step 2: LLM summary estructurado — requiere provider.
	if r.provider == nil {
		return nil
	}
	// Si thrashing y no forzado, solo prune ya hecho, no LLM.
	if sess.CompactionCount >= 3 && !forced {
		return nil
	}
	summary, err := summarizeLocal(ctx, r.provider, sess.Model, *sess, focus)
	if err != nil {
		return err
	}
	// Validación post-hoc: el summary debe preservar lo crítico (≥3 secciones).
	// Si viene malformado, 1 retry determinista antes de aceptar (autónomo, sin pausar).
	if !session.ValidCompactionSummary(summary) {
		r.logger.Warn("compaction.summary_invalid_retry", "session", sess.ID, "chars", len(summary))
		if retry, rerr := summarizeLocal(ctx, r.provider, sess.Model, *sess, focus); rerr == nil && session.ValidCompactionSummary(retry) {
			summary = retry
		}
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
	r.messenger.Notice(sess.ID, fmt.Sprintf("Compacted session history — summary injected, tail preserved (%d msgs)", len(sess.Messages)-sess.CompactBoundary))
	r.logger.Info("compaction.summary", "session", sess.ID, "boundary", sess.CompactBoundary, "chars", len(summary))
	// Decaimiento: si bajamos de 70% tras compactar, reset del contador.
	r.decayCompactionCount(sess, systemPrompt, tools)
	return nil
}

// decayCompactionCount resetea el contador anti-thrashing cuando los tokens
// bajan de 70%: evita que el auto quede bloqueado para siempre en sesiones largas.
func (r *Runner) decayCompactionCount(sess *domain.Session, systemPrompt string, tools []domain.Tool) {
	if sess.CompactionCount == 0 {
		return
	}
	if session.UsageRatio(*sess, systemPrompt, tools, sess.Model, r.compaction.ModelMetadata) < session.WarnThreshold {
		sess.CompactionCount = 0
		_ = r.sessions.Save(context.Background(), *sess)
		r.logger.Info("compaction.count_reset", "session", sess.ID)
	}
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

// Helpers locales hacia session (centralizado, evita drift).
func needsPruneLocal(s domain.Session) (bool, int) {
	return session.NeedsPrune(s)
}

func pruneLocal(s domain.Session) (domain.Session, int) {
	return session.Prune(s)
}

func summarizeLocal(ctx context.Context, provider ports.LLMProvider, model domain.Model, s domain.Session, focus string) (string, error) {
	return session.NewCompactionService(provider, model).Summarize(ctx, s, focus)
}

func applyCompactionLocal(s domain.Session, summary string) domain.Session {
	return session.ApplyCompaction(s, summary)
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
	for line := range strings.SplitSeq(patch, "\n") {
		if after, ok := strings.CutPrefix(line, "*** Update File:"); ok {
			return strings.TrimSpace(after)
		}
		if after, ok := strings.CutPrefix(line, "*** Add File:"); ok {
			return strings.TrimSpace(after)
		}
		if after, ok := strings.CutPrefix(line, "+++ b/"); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

// rememberAllow persiste "permitir siempre" para el resto de la sesión:
// añade una regla Auto al decisor (si lo soporta).
func (r *Runner) rememberAllow(call domain.ToolCall) {
	type ruleAdder interface {
		AddRule(domain.PermissionRule)
	}
	if adder, ok := r.decider.(ruleAdder); ok {
		adder.AddRule(domain.PermissionRule{
			Tool:      call.Name,
			Arguments: call.Arguments,
			Level:     domain.PermissionAuto,
			IsExact:   true,
		})
	}
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
