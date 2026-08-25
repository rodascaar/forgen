// Package session — compaction implementa el estándar Extract 2-step:
// Opencode prune no-destructivo (40k protect / 20k minimum) + Claude tool trim
// + LLM summary 5 headings. Cache-friendly: solo tail se toca.
package session

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

const (
	// PruneProtectTokens protege los últimos N tokens de tool output del pruning.
	PruneProtectTokens = 40000
	// PruneMinimumTokens mínimo pruneable para que valga la pena el trabajo.
	PruneMinimumTokens = 20000
	// DefaultContextLimit fallback cuando ModelMetadata.ContextLimit no está definido.
	DefaultContextLimit = 128000
	// DefaultReservedTokens tokens reservados para max_output.
	DefaultReservedTokens = 4096
	// SummaryPlaceholder reemplaza tool outputs pruneados (Claude Capa1).
	SummaryPlaceholder = "[tool result cleared — use read/grep to re-fetch if needed]"
	// CompactionThrashingLimit compactaciones consecutivas sin progreso antes de pausar.
	CompactionThrashingLimit = 3
	// DefaultCompactionThreshold umbral default (85%).
	DefaultCompactionThreshold = 0.85
)

// EstimateTokens estima tokens ~ chars/4 (heurística barata, agnóstica).
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return len(text)/4 + 1
}

// MessageTokens estima tokens de un mensaje (rol + contenido).
func MessageTokens(m domain.Message) int {
	n := 4 // overhead rol
	for _, p := range m.Content {
		n += EstimateTokens(p.Text)
		if p.Call != nil {
			n += EstimateTokens(p.Call.Name)
			for _, v := range p.Call.Arguments {
				n += EstimateTokens(fmt.Sprintf("%v", v))
			}
		}
	}
	n += EstimateTokens(m.ToolName)
	n += EstimateTokens(m.ToolCallID)
	return n
}

// SessionTokens estima tokens de la sesión (sin system prompt).
func SessionTokens(s domain.Session) int {
	total := 0
	for _, m := range s.Messages {
		total += MessageTokens(m)
	}
	return total
}

// ContextLimitFor devuelve el límite de contexto efectivo para un modelo.
func ContextLimitFor(model domain.Model, metadata map[string]domain.ModelMetadata) int {
	if md, ok := metadata[model.Key()]; ok && md.ContextLimit > 0 {
		return md.ContextLimit
	}
	return DefaultContextLimit
}

// ReservedFor devuelve tokens reservados para output.
func ReservedFor(model domain.Model, metadata map[string]domain.ModelMetadata) int {
	if md, ok := metadata[model.Key()]; ok && md.MaxOutput > 0 {
		return md.MaxOutput
	}
	return DefaultReservedTokens
}

// IsOverflow decide si la sesión necesita compactación (Opencode isOverflow pattern).
// threshold 0.85 significa: tokens > (limit - reserved) * 0.85
func IsOverflow(session domain.Session, model domain.Model, metadata map[string]domain.ModelMetadata, threshold float64) bool {
	if threshold == 0 {
		threshold = DefaultCompactionThreshold
	}
	limit := ContextLimitFor(model, metadata)
	reserved := ReservedFor(model, metadata)
	usable := limit - reserved
	if usable <= 0 {
		usable = limit
	}
	budget := int(float64(usable) * threshold)
	return SessionTokens(session) >= budget
}

// NeedsPrune indica si hay suficiente contenido pruneable para justificar trabajo.
func NeedsPrune(session domain.Session) (bool, int) {
	pruneable := countPruneableTokens(session)
	return pruneable >= PruneMinimumTokens, pruneable
}

func countPruneableTokens(session domain.Session) int {
	// Calcular ventana protegida: últimos PruneProtectTokens de tool outputs + 2 turnos usuario.
	protected := protectedIndices(session)
	pruneable := 0
	for i, m := range session.Messages {
		if protected[i] {
			continue
		}
		if m.Role == domain.RoleTool && m.CompactedAt == nil {
			pruneable += MessageTokens(m)
		}
	}
	return pruneable
}

// protectedIndices marca mensajes protegidos del pruning.
func protectedIndices(session domain.Session) map[int]bool {
	protected := make(map[int]bool)
	// Proteger últimos PruneProtectTokens de tool outputs (recorrer desde el final).
	acc := 0
	for i := len(session.Messages) - 1; i >= 0; i-- {
		m := session.Messages[i]
		if m.Role == domain.RoleTool {
			if acc < PruneProtectTokens {
				protected[i] = true
				acc += MessageTokens(m)
			}
		}
	}
	// Proteger últimos 2 turnos de usuario (y su siguiente assistant/tool).
	userTurns := 0
	for i := len(session.Messages) - 1; i >= 0 && userTurns < 2; i-- {
		if session.Messages[i].Role == domain.RoleUser {
			protected[i] = true
			userTurns++
			// También proteger assistant inmediato siguiente si existe y no es tool.
			if i+1 < len(session.Messages) {
				protected[i+1] = true
			}
		}
	}
	// Skill outputs nunca se prunean (ToolName == "read_skill").
	for i, m := range session.Messages {
		if m.ToolName == "read_skill" {
			protected[i] = true
		}
	}
	return protected
}

// Prune marca mensajes tool antiguos con CompactedAt (no destructivo, reversible).
// Retorna sesión pruneada y cuántos mensajes se marcaron.
func Prune(session domain.Session) (domain.Session, int) {
	protected := protectedIndices(session)
	now := time.Now()
	marked := 0
	for i := range session.Messages {
		if protected[i] {
			continue
		}
		m := &session.Messages[i]
		if m.Role == domain.RoleTool && m.CompactedAt == nil {
			t := now
			m.CompactedAt = &t
			marked++
		}
	}
	if marked > 0 {
		session.CompactionCount++
	}
	return session, marked
}

// VisibleMessages devuelve la vista filtrada para el LLM:
// - Mensajes pruneados: ToolResult reemplazado por placeholder (preserva ToolCallID para flow)
// - Si hay CompactBoundary, mensajes < boundary se colapsan en summary sintético.
func VisibleMessages(session domain.Session) []domain.Message {
	// Si hay summary y boundary, inyectar summary al inicio.
	var out []domain.Message
	if session.CompactBoundary > 0 && session.CompactionSummary != "" {
		summaryMsg := domain.Message{
			Role:    domain.RoleSystem,
			Content: []domain.ContentPart{{Type: "text", Text: summaryPrefix(session.CompactionSummary)}},
			CreatedAt: time.Now(),
		}
		// Marcar como summary para que storage no lo re-prune.
		summaryMsg.IsSummary = true
		out = append(out, summaryMsg)
		// Mensajes desde boundary.
		for i := session.CompactBoundary; i < len(session.Messages); i++ {
			out = append(out, projectMessage(session.Messages[i]))
		}
		return out
	}
	for _, m := range session.Messages {
		out = append(out, projectMessage(m))
	}
	return out
}

func projectMessage(m domain.Message) domain.Message {
	if m.CompactedAt != nil && m.Role == domain.RoleTool {
		// Reemplazar contenido por placeholder, preservar IDs para correlación.
		return domain.Message{
			Role:       m.Role,
			ToolCallID: m.ToolCallID,
			ToolName:   m.ToolName,
			Content:    []domain.ContentPart{{Type: "text", Text: SummaryPlaceholder}},
			CreatedAt:  m.CreatedAt,
		}
	}
	return m
}

func summaryPrefix(summary string) string {
	return "Another language model started to solve this task and produced a summary. Use it to build on work already done and avoid duplicating effort.\n\n## Session Summary (compacted)\n" + strings.TrimSpace(summary)
}

// DetectLanguage heurística simple es→en para prompt de compaction.
func DetectLanguage(session domain.Session) string {
	for _, m := range session.Messages {
		if m.Role == domain.RoleUser {
			t := strings.ToLower(m.Text())
			// Heurística: palabras clave español.
			if strings.Contains(t, "añad") || strings.Contains(t, "crea") || strings.Contains(t, "página") || strings.Contains(t, "implementa") || strings.Contains(t, "corrige") {
				return "es"
			}
			break
		}
	}
	return "en"
}

// CompactionPrompts prompts bilingües 5 headings (Opencode base).
var CompactionPromptES = `Eres un asistente que resume conversaciones para continuar la sesión.

Genera un resumen detallado pero conciso. Esta será la ÚNICA memoria disponible al continuar, así que preserva información crítica:

- Qué se hizo (tareas completadas, decisiones)
- En qué se está trabajando ahora
- Qué archivos se modificaron y su estado
- Qué falta por hacer (próximos pasos claros)
- Peticiones/restricciones clave del usuario y decisiones técnicas con su porqué

Sé conciso pero suficiente para continuar sin perder contexto. Si hay instrucciones de enfoque, prioriza ese foco.`

var CompactionPromptEN = `You are an assistant that summarizes conversations to continue the session.

Generate a detailed but concise summary. This will be the ONLY memory when continuing, so preserve critical information:

- What was done (completed tasks, decisions)
- What is currently being worked on
- Which files were modified and their status
- What remains to be done (clear next steps)
- Key user requests/constraints and technical decisions with rationale

Be concise but sufficient to continue without losing context. If focus instructions are provided, prioritize that focus.`

func CompactionPromptFor(lang string) string {
	if lang == "es" {
		return CompactionPromptES
	}
	return CompactionPromptEN
}

func CompactionUserMessageFor(lang string, focus string) string {
	baseES := "Resume nuestra conversación anterior. Este resumen será el único contexto disponible al continuar, así que preserva: qué se logró, trabajo en progreso, archivos involucrados, próximos pasos y peticiones/restricciones clave. Sé conciso pero detallado para continuar sin fricción."
	baseEN := "Summarize our conversation above. This summary will be the only context available when continuing, so preserve: what was accomplished, work in progress, files involved, next steps and key requests/constraints. Be concise but detailed to continue seamlessly."
	var base string
	if lang == "es" {
		base = baseES
	} else {
		base = baseEN
	}
	if strings.TrimSpace(focus) != "" {
		if lang == "es" {
			return base + "\n\nEnfoque solicitado: " + focus
		}
		return base + "\n\nFocus requested: " + focus
	}
	return base
}

// Service de compactación con inyección de LLM.

type CompactionService struct {
	provider ports.LLMProvider
	model    domain.Model
}

func NewCompactionService(provider ports.LLMProvider, model domain.Model) *CompactionService {
	return &CompactionService{provider: provider, model: model}
}

// Summarize genera el resumen LLM (5 headings) usando la sesión visible pre-compact.
func (c *CompactionService) Summarize(ctx context.Context, session domain.Session, focus string) (string, error) {
	lang := DetectLanguage(session)
	systemPrompt := CompactionPromptFor(lang)
	userMsg := CompactionUserMessageFor(lang, focus)

	// Construir historial para el LLM: mensajes visibles (con placeholders) + userMsg de resumen.
	msgs := VisibleMessages(session)
	// Filtro: no enviar system previos, solo user/assistant/tool visibles.
	var llmMsgs []domain.Message
	llmMsgs = append(llmMsgs, domain.NewTextMessage(domain.RoleSystem, systemPrompt))
	llmMsgs = append(llmMsgs, msgs...)
	llmMsgs = append(llmMsgs, domain.NewTextMessage(domain.RoleUser, userMsg))

	var summary strings.Builder
	req := ports.ChatRequest{
		Model:       c.model,
		Messages:    llmMsgs,
		Temperature: 0.2,
		MaxTokens:   2048,
	}
	err := c.provider.StreamChat(ctx, req, func(ev ports.StreamEvent) error {
		if d, ok := ev.(ports.TextDeltaEvent); ok {
			summary.WriteString(d.Text)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("compaction summarize: %w", err)
	}
	out := strings.TrimSpace(summary.String())
	if out == "" {
		return "", fmt.Errorf("compaction: resumen vacío")
	}
	return out, nil
}

// ApplyCompaction aplica pruning + summary y actualiza boundary.
// Si prune solo es suficiente (baja tokens < threshold) puede no requerir LLM — caller decide.
func ApplyCompaction(session domain.Session, summary string) domain.Session {
	// Boundary = índice desde donde se conserva tail (después de pruning, tail = protegidos + recientes).
	// Heurística: conservar últimos 20 mensajes como tail (coherente con PruneProtectTokens).
	tail := 20
	if len(session.Messages) < tail {
		tail = len(session.Messages)
	}
	session.CompactBoundary = len(session.Messages) - tail
	if session.CompactBoundary < 0 {
		session.CompactBoundary = 0
	}
	session.CompactionSummary = strings.TrimSpace(summary)
	session.CompactionCount++
	return session
}

// IsThrashing detecta 3 compactions seguidas sin reducción suficiente.
func IsThrashing(session domain.Session) bool {
	return session.CompactionCount >= CompactionThrashingLimit
}
