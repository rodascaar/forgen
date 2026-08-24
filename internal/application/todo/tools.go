package todo

import (
	"context"
	"fmt"
	"strings"

	"github.com/rodascaar/forgen/internal/application/tools"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

const ToolName = "todowrite"

func NewTool(store ports.TodoStore) tools.ToolDef {
	return tools.NewGenericTool[struct {
		Todos []struct {
			Content    string `json:"content"`
			Status     string `json:"status"`
			ActiveForm string `json:"activeForm"`
		} `json:"todos"`
	}](ToolName,
		"Crea y mantiene una lista estructurada de tareas. Úsala para planificar trabajo multi-paso, dar visibilidad al usuario y no olvidar pasos. Mantén exactamente UN 'in_progress' a la vez.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"todos": map[string]any{
					"type":        "array",
					"description": "Lista completa y actualizada de tareas",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"content":    map[string]any{"type": "string", "description": "Descripción breve de la tarea"},
							"status":     map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed", "cancelled"}},
							"activeForm": map[string]any{"type": "string", "description": "Forma activa/gerundio, ej 'Creando archivo X'"},
						},
						"required": []string{"content", "status"},
					},
				},
			},
			"required": []string{"todos"},
		},
		func(ctx context.Context, args struct {
			Todos []struct {
				Content    string `json:"content"`
				Status     string `json:"status"`
				ActiveForm string `json:"activeForm"`
			} `json:"todos"`
		}) domain.ToolResult {
			if len(args.Todos) == 0 {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("todowrite: la lista no puede estar vacía")}
			}
			inProgress := 0
			for _, t := range args.Todos {
				if strings.TrimSpace(t.Content) == "" {
					return domain.ToolResult{OK: false, Error: fmt.Errorf("todowrite: entrada sin 'content'")}
				}
				switch t.Status {
				case "pending", "in_progress", "completed", "cancelled":
				default:
					return domain.ToolResult{OK: false, Error: fmt.Errorf("todowrite: estado inválido %q", t.Status)}
				}
				if t.Status == "in_progress" {
					inProgress++
				}
			}
			if inProgress > 1 {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("todowrite: solo puede haber un 'in_progress' a la vez (hay %d)", inProgress)}
			}
			const listID = "default"
			list, err := store.Load(ctx, listID)
			if err != nil {
				list = &domain.TodoList{ID: listID, Name: "default", Todos: make([]*domain.Todo, 0, len(args.Todos))}
			} else {
				list.Todos = make([]*domain.Todo, 0, len(args.Todos))
			}
			for _, e := range args.Todos {
				status := domain.TodoStatus(e.Status)
				if e.Status == "completed" {
					status = domain.TodoStatusDone
				}
				t := domain.NewTodo(e.Content, e.ActiveForm)
				t.Status = status
				list.Todos = append(list.Todos, t)
			}
			inProgress = 0
			for _, t := range list.Todos {
				if t.Status == domain.TodoStatusInProgress {
					inProgress++
				}
			}
			if inProgress > 1 {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("todowrite: solo puede haber un 'in_progress' a la vez (hay %d)", inProgress)}
			}
			if err := store.Save(ctx, list); err != nil {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("todowrite: guardar: %w", err)}
			}
			var sb strings.Builder
			for i, t := range list.Todos {
				icon := " "
				switch t.Status {
				case domain.TodoStatusDone:
					icon = "✓"
				case domain.TodoStatusInProgress:
					icon = "▸"
				case domain.TodoStatusCancelled:
					icon = "✗"
				}
				fmt.Fprintf(&sb, "%d. %s %s [%s]\n", i+1, icon, t.Content, t.Status)
			}
			return domain.ToolResult{OK: true, Output: sb.String()}
		},
	)
}
