package task

import (
	"context"
	"fmt"

	"github.com/rodascaar/forgen/internal/application/tools"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

const ToolName = "task"

func NewTool(store ports.TaskStore, executor ports.TaskExecutor) tools.ToolDef {
	return tools.NewGenericTool[struct {
		Description     string `json:"description"`
		Prompt          string `json:"prompt"`
		SubagentType    string `json:"subagent_type"`
		RunInBackground bool   `json:"run_in_background,omitempty"`
	}](ToolName,
		"Lanza un sub-agente. WHEN_TO_USE: delega explore/plan/build/review/research. Para tareas 3+ pasos usa varios task calls en paralelo (el runner los ejecuta con 5 concurrent). Usa run_in_background para tareas largas y consulta con 'forgen task list'. Ejemplo: {\"description\":\"Explorar auth\",\"prompt\":\"Busca lógica auth en src/\",\"subagent_type\":\"explore\"}.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"description":       map[string]any{"type": "string", "description": "Descripción corta (3-5 palabras)"},
				"prompt":            map[string]any{"type": "string", "description": "Prompt detallado para el sub-agente"},
				"subagent_type":     map[string]any{"type": "string", "enum": []string{"explore", "plan", "build", "review", "research"}, "description": "Tipo de sub-agente"},
				"run_in_background": map[string]any{"type": "boolean", "description": "Si true, lanza en background y devuelve task ID sin esperar"},
			},
			"required": []string{"description", "prompt", "subagent_type"},
		},
		func(ctx context.Context, args struct {
			Description     string `json:"description"`
			Prompt          string `json:"prompt"`
			SubagentType    string `json:"subagent_type"`
			RunInBackground bool   `json:"run_in_background,omitempty"`
		}) domain.ToolResult {
			tType := domain.TaskType(args.SubagentType)
			if tType == "" {
				tType = domain.TaskTypeBuild
			}
			reg := domain.DefaultSubAgentRegistry()
			cfg, ok := reg.GetAgentConfig(tType)
			if !ok {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("tipo %q no existe", args.SubagentType)}
			}
			task := domain.NewTask(tType, args.Description, args.Prompt, cfg)
			if err := store.Save(ctx, task); err != nil {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("guardar task: %w", err)}
			}
			if args.RunInBackground {
				go func() { _, _ = executor.Execute(context.WithoutCancel(ctx), task) }() //nolint:gosec // G118 detached background task
				return domain.ToolResult{OK: true, Output: fmt.Sprintf("Task %s lanzada en background (subagent_type=%s). Usa 'forgen task list' o 'task status %s'", task.ID, tType, task.ID)}
			}
			// Ejecutar sincrónicamente y devolver resultado
			result, err := executor.Execute(ctx, task)
			if err != nil {
				return domain.ToolResult{OK: false, Error: err, Output: task.Error}
			}
			out := result.Summary
			if result.Output != "" {
				out += "\n" + result.Output
			}
			return domain.ToolResult{OK: true, Output: out}
		},
	)
}
