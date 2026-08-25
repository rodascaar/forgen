package domain

import (
	"embed"
	"strings"
	"time"
)

//go:embed prompts/*.md
var promptFS embed.FS

func loadPrompt(name string) string {
	data, err := promptFS.ReadFile("prompts/" + name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// PromptFor returns the embedded prompt for agent+lang; fallback to legacy hardcoded.
func PromptFor(agent, lang string) string {
	lang = strings.ToLower(lang)
	if lang != "es" && lang != "en" {
		lang = "en"
	}
	name := agent + "." + lang + ".md"
	if p := loadPrompt(name); p != "" {
		return p
	}
	// fallback to embedded legacy
	for _, a := range BuiltinAgents() {
		if a.Name == agent {
			return a.SystemPrompt
		}
	}
	return ""
}

// Agent define la personalidad y el alcance de un agente.
type Agent struct {
	Name         string
	Description  string
	IsReadOnly   bool
	SystemPrompt string
	// AllowedTools es vacío si se permiten todas.
	AllowedTools []string
	DeniedTools  []string
}

// CanUseTool decide si el agente tiene permitido usar una herramienta.
func (a Agent) CanUseTool(toolName string) bool {
	for _, denied := range a.DeniedTools {
		if denied == toolName {
			return false
		}
	}
	if len(a.AllowedTools) == 0 {
		return true
	}
	for _, allowed := range a.AllowedTools {
		if allowed == toolName {
			return true
		}
	}
	return false
}

// BuiltinAgents devuelve los agentes integrados (legacy prompts inline; prefer PromptFor with embedded files).
func BuiltinAgents() []Agent {
	return BuiltinAgentsForLang("en")
}

// BuiltinAgentsForLang devuelve agentes con prompts embebidos por idioma (es/en).
func BuiltinAgentsForLang(lang string) []Agent {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang != "es" && lang != "en" {
		lang = "en"
	}
	buildPrompt := loadPrompt("build." + lang + ".md")
	if buildPrompt == "" {
		buildPrompt = "You are forgen build agent. Explore with glob/grep/read, plan with todowrite, implement with write/edit/apply_patch, verify with bash/lsp."
	}
	planPrompt := loadPrompt("plan." + lang + ".md")
	if planPrompt == "" {
		planPrompt = "You are forgen plan agent (read-only). Analyze and explore only."
	}
	return []Agent{
		{
			Name:         "build",
			Description:  "Agente de desarrollo con acceso total (lee, escribe y ejecuta).",
			IsReadOnly:   false,
			SystemPrompt: buildPrompt,
		},
		{
			Name:         "plan",
			Description:  "Agente de análisis y exploración de solo lectura.",
			IsReadOnly:   true,
			SystemPrompt: planPrompt,
			DeniedTools:  []string{"write", "edit", "bash", "apply_patch", "task", "lsp_rename", "todo"},
		},
	}
}

// ResolveLanguage normaliza es/en.
func ResolveLanguage(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "es" {
		return "es"
	}
	return "en"
}

// FindAgent busca un agente por nombre en la lista.
func FindAgent(agents []Agent, name string) (Agent, bool) {
	for _, agent := range agents {
		if agent.Name == name {
			return agent, true
		}
	}
	return Agent{}, false
}

// Session identifica una conversación persistida.
type Session struct {
	ID        string
	Workspace string
	Model     Model
	Agent     string
	StartedAt time.Time
	UpdatedAt time.Time
	Messages  []Message
	// Summary es una descripción corta cacheada (primer mensaje de usuario).
	SummaryCache string
	// CompactBoundary es el índice en Messages donde empieza el tail post-compaction.
	// -1 = sin compactación. Mensajes < boundary son historia compactada (vista filtrada).
	CompactBoundary int `json:"compact_boundary,omitempty"`
	// CompactionCount cuenta compactions consecutivas para anti-thrashing.
	CompactionCount int `json:"compaction_count,omitempty"`
	// CompactionSummary guarda el último resumen para reconstrucción sin LLM extra.
	CompactionSummary string `json:"compaction_summary,omitempty"`
}

// LastMessage devuelve el último mensaje de la sesión, si existe.
func (s Session) LastMessage() (Message, bool) {
	if len(s.Messages) == 0 {
		return Message{}, false
	}
	return s.Messages[len(s.Messages)-1], true
}

// Summary devuelve una descripción corta de la sesión basada en el primer mensaje de usuario.
func (s Session) Summary() string {
	if s.SummaryCache != "" {
		return s.SummaryCache
	}
	for _, message := range s.Messages {
		if message.Role == RoleUser {
			text := message.Text()
			if len(text) > 80 {
				text = text[:80] + "..."
			}
			return text
		}
	}
	return "sesión sin mensajes"
}
