package domain

import "time"

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

// BuiltinAgents devuelve los agentes integrados.
func BuiltinAgents() []Agent {
	return []Agent{
		{
			Name:         "build",
			Description:  "Agente de desarrollo con acceso total (lee, escribe y ejecuta).",
			IsReadOnly:   false,
			SystemPrompt: "Eres forgen, un agente de desarrollo que ayuda a los usuarios a escribir, refactorizar y depurar código. Trabajas en el workspace del proyecto actual. Prefiere herramientas de lectura (glob/grep/read) antes de adivinar. Ejecuta comandos con bash para validar. Eres conciso y preciso.",
		},
		{
			Name:         "plan",
			Description:  "Agente de análisis y exploración de solo lectura.",
			IsReadOnly:   true,
			SystemPrompt: "Eres forgen en modo plan. Solo puedes analizar y explorar el código: leer, buscar y revisar. No puedes escribir archivos ni ejecutar comandos que modifiquen el sistema. Produce un análisis claro con hallazgos y recomendaciones.",
			DeniedTools:  []string{"write", "edit", "bash"},
		},
	}
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
