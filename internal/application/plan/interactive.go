package plan

import (
	"context"
	"fmt"
	"strings"

	"github.com/rodascaar/forgen/internal/application/tools"
	"github.com/rodascaar/forgen/internal/core/domain"
)

const AskToolName = "ask_question"
const ExitPlanToolName = "exit_plan_mode"

// NewAskTool crea la herramienta ask_question (claude-code AskUserQuestion).
func NewAskTool() tools.ToolDef {
	return tools.NewGenericTool[struct {
		Questions []struct {
			Question string   `json:"question"`
			Header   string   `json:"header"`
			Options  []string `json:"options"`
			Multi    bool     `json:"multiSelect,omitempty"`
		} `json:"questions"`
	}](AskToolName,
		"Hace preguntas al usuario para aclarar requisitos. WHEN_TO_USE: cuando hay ambigüedad que bloquea el plan (ej: elegir stack, alcance). El usuario responde vía TUI. Ejemplo: {\"questions\":[{\"question\":\"¿Qué stack prefieres?\",\"header\":\"Stack\",\"options\":[\"Next.js\",\"Vite\"],\"multiSelect\":false}]}",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"questions": map[string]any{
					"type": "array", "description": "1-4 preguntas",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"question":    map[string]any{"type": "string", "description": "Pregunta completa"},
							"header":      map[string]any{"type": "string", "description": "Header corto (1-3 palabras)"},
							"options":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "2-4 opciones"},
							"multiSelect": map[string]any{"type": "boolean", "description": "Permitir múltiples selecciones"},
						},
						"required": []string{"question", "header", "options"},
					},
				},
			},
			"required": []string{"questions"},
		},
		func(ctx context.Context, args struct {
			Questions []struct {
				Question string   `json:"question"`
				Header   string   `json:"header"`
				Options  []string `json:"options"`
				Multi    bool     `json:"multiSelect,omitempty"`
			} `json:"questions"`
		}) domain.ToolResult {
			if len(args.Questions) == 0 {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("ask_question: al menos 1 pregunta requerida")}
			}
			if len(args.Questions) > 4 {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("ask_question: máximo 4 preguntas")}
			}
			var sb strings.Builder
			sb.WriteString("Preguntas para el usuario (responde vía TUI):\n")
			for i, q := range args.Questions {
				fmt.Fprintf(&sb, "%d. [%s] %s\n   Opciones: %s\n", i+1, q.Header, q.Question, strings.Join(q.Options, " | "))
			}
			sb.WriteString("\n[El usuario debe responder antes de continuar el plan]")
			return domain.ToolResult{OK: true, Output: sb.String()}
		},
	)
}

// NewExitPlanTool crea exit_plan_mode (opencode/claude).
func NewExitPlanTool() tools.ToolDef {
	return tools.NewGenericTool[struct {
		Plan string `json:"plan"`
	}](ExitPlanToolName,
		"Sale del modo plan tras presentar el plan al usuario. WHEN_TO_USE: solo cuando el plan está completo y el usuario ha validado. El plan se guarda en .forgen/plans/plan.md",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"plan": map[string]any{"type": "string", "description": "Resumen final del plan (markdown)"},
			},
			"required": []string{"plan"},
		},
		func(ctx context.Context, args struct {
			Plan string `json:"plan"`
		}) domain.ToolResult {
			if strings.TrimSpace(args.Plan) == "" {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("exit_plan_mode: plan vacío")}
			}
			return domain.ToolResult{OK: true, Output: "Plan aprobado. Saliendo de modo plan. Usa Tab para cambiar a build o ejecuta el plan."}
		},
	)
}
