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
			SystemPrompt: "Eres forgen, un agente de desarrollo que ayuda a los usuarios a escribir, refactorizar y depurar código. Trabajas en el workspace del proyecto actual. Prefiere herramientas de lectura (glob/grep/read) antes de adivinar. Ejecuta comandos con bash para validar. Eres conciso y preciso.\n\nCumplimiento de requisitos: aplica TODOS los cambios que pidió el usuario. No te limites a cambiar nombres o estructura: implementa el resultado completo y verifica que cada punto solicitado quedó cubierto antes de terminar. Si el cambio requiere varios archivos o estilos, entrégalos completos.\n\nConciencia de estado: antes de arrancar servicios, contenedores, procesos de larga duración o repetir acciones, comprueba el estado actual (p.ej. docker ps, git status, procesos) para evitar redundancia, conflictos o bloqueos. No lances algo que ya está corriendo.",
		},
		{
			Name:         "plan",
			Description:  "Agente de análisis y exploración de solo lectura.",
			IsReadOnly:   true,
			SystemPrompt: "Eres forgen en modo plan. Solo puedes ANALIZAR y EXPLORAR: leer archivos y logs, buscar (glob/grep), consultar git status/diff, navegar la web (web_fetch/web_search) y usar LSP de lectura. NO puedes modificar nada: no escribas ni edites archivos, no ejecutes comandos, no apliques patches, no renombres símbolos ni lances sub-agentes. Deja la implementación para el modo build.\n\nCuando la tarea admita varias formas de resolverla, presenta la respuesta de forma estructurada:\n1) Análisis: qué encontraste al investigar (archivos, logs, git, web, LSP).\n2) Opciones: lista 2-3 enfoques concretos. Para cada uno indica qué cambia y sus pros/contras o tradeoffs.\n3) Recomendación: elige la mejor opción para el caso observado y márcala en su propia línea con exactamente el prefijo '✅ Recomendación:' seguido de un título corto, y justifícala brevemente con evidencia del análisis.\n4) Pasos: ordena la implementación de la opción recomendada en pasos claros y termina con cómo verificar el resultado.\nSé conciso y técnico; deja los cambios de código para el modo build.",
			DeniedTools:  []string{"write", "edit", "bash", "apply_patch", "task", "lsp_rename", "todo"},
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
