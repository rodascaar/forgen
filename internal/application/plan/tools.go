package plan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rodascaar/forgen/internal/application/tools"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

const ToolName = "update_plan"

// NewTool creates the update_plan tool (Codex-style FSM 1-7 steps, exactly 1 in_progress).
// Uses same TodoStore but with list ID "plan" to keep plan separate from generic todos.
func NewTool(store ports.TodoStore) tools.ToolDef {
	return tools.NewGenericTool[struct {
		Steps []struct {
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"steps"`
		Explanation string `json:"explanation,omitempty"`
	}](ToolName,
		"Gestiona el plan de implementación como lista estructurada FSM (Codex-style). WHEN_TO_USE: tareas 3+ pasos — crea plan al inicio, marca exactamente 1 'in_progress' a la vez, actualiza status a 'completed' antes de avanzar. Para 9B: esta herramienta es tu memoria — no avances sin actualizar plan. Ejemplo: {\"steps\":[{\"content\":\"Explorar repo\",\"status\":\"completed\"},{\"content\":\"Implementar /dashboard\",\"status\":\"in_progress\"},{\"content\":\"Verificar build\",\"status\":\"pending\"}]}. Máx 7 pasos, cada content 1-7 palabras ideal. Si violas (0 o 2+ in_progress) falla.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"steps": map[string]any{
					"type": "array", "description": "Plan completo actualizado (1-7 pasos), no delta",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"content": map[string]any{"type": "string", "description": "Título corto del paso (1-7 palabras, ej: 'Crear página dashboard')"},
							"status":  map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed"}},
						},
						"required": []string{"content", "status"},
					},
				},
				"explanation": map[string]any{"type": "string", "description": "Breve justificación del cambio de plan (opcional)"},
			},
			"required": []string{"steps"},
		},
		func(ctx context.Context, args struct {
			Steps []struct {
				Content string `json:"content"`
				Status  string `json:"status"`
			} `json:"steps"`
			Explanation string `json:"explanation,omitempty"`
		}) domain.ToolResult {
			if len(args.Steps) == 0 {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("update_plan: steps no puede estar vacío")}
			}
			if len(args.Steps) > 7 {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("update_plan: máximo 7 pasos (recibidos %d)", len(args.Steps))}
			}
			inProgress := 0
			for _, s := range args.Steps {
				if strings.TrimSpace(s.Content) == "" {
					return domain.ToolResult{OK: false, Error: fmt.Errorf("update_plan: paso sin content")}
				}
				switch s.Status {
				case "pending", "in_progress", "completed":
				default:
					return domain.ToolResult{OK: false, Error: fmt.Errorf("update_plan: status inválido %q", s.Status)}
				}
				if s.Status == "in_progress" {
					inProgress++
				}
			}
			if inProgress == 0 {
				// Permitir 0 solo si todo completed (plan terminado)
				allDone := true
				for _, s := range args.Steps {
					if s.Status != "completed" {
						allDone = false
						break
					}
				}
				if !allDone {
					return domain.ToolResult{OK: false, Error: fmt.Errorf("update_plan: debe haber exactamente 1 'in_progress' (hay 0)")}
				}
			}
			if inProgress > 1 {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("update_plan: solo 1 'in_progress' a la vez (hay %d)", inProgress)}
			}
			const listID = "plan"
			list, err := store.Load(ctx, listID)
			if err != nil {
				list = &domain.TodoList{ID: listID, Name: "plan", Todos: make([]*domain.Todo, 0, len(args.Steps))}
			} else {
				list.Todos = make([]*domain.Todo, 0, len(args.Steps))
			}
			for _, s := range args.Steps {
				t := domain.NewTodo(s.Content, "")
				switch s.Status {
				case "pending":
					t.Status = domain.TodoStatusPending
				case "in_progress":
					t.Status = domain.TodoStatusInProgress
				case "completed":
					t.Status = domain.TodoStatusDone
				}
				list.Todos = append(list.Todos, t)
			}
			if err := store.Save(ctx, list); err != nil {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("update_plan: guardar: %w", err)}
			}
			// Plan artifact .forgen/plans/plan.md versionado (7.5.4)
			_ = writePlanArtifact(list, args.Explanation)
			var sb strings.Builder
			sb.WriteString("Plan actualizado:\n")
			for i, t := range list.Todos {
				icon := "○"
				switch t.Status {
				case domain.TodoStatusDone:
					icon = "✓"
				case domain.TodoStatusInProgress:
					icon = "▸"
				}
				fmt.Fprintf(&sb, "%d. %s %s [%s]\n", i+1, icon, t.Content, t.Status)
			}
			if args.Explanation != "" {
				sb.WriteString("Nota: " + args.Explanation + "\n")
			}
			sb.WriteString("\nArtifact: .forgen/plans/plan.md\n")
			return domain.ToolResult{OK: true, Output: sb.String()}
		},
	)
}

func writePlanArtifact(list *domain.TodoList, explanation string) error {
	dir := ".forgen/plans"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, "plan.md")
	var sb strings.Builder
	sb.WriteString("# Plan — forgen\n\n")
	if explanation != "" {
		sb.WriteString("_" + explanation + "_\n\n")
	}
	for i, t := range list.Todos {
		status := string(t.Status)
		if t.Status == domain.TodoStatusDone {
			status = "completed"
		}
		fmt.Fprintf(&sb, "%d. [%s] %s\n", i+1, status, t.Content)
	}
	sb.WriteString("\n---\n_Generado por update_plan (Fase 7.5.4)_\n")
	return os.WriteFile(path, []byte(sb.String()), 0644) //nolint:gosec // G306 plan artifact workspace file
}
