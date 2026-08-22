package cli

import (
	"context"
	"fmt"
	"strconv"

	"github.com/rodascaar/forgen/internal/app"
	"github.com/rodascaar/forgen/internal/application/todo"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/spf13/cobra"
)

func newTodoCommand(app *app.App) *cobra.Command {
	cmd := &cobra.Command{Use: "todo", Short: "Gestiona listas de tareas"}
	cmd.AddCommand(newTodoListCmd(app))
	cmd.AddCommand(newTodoAddCmd(app))
	cmd.AddCommand(newTodoDoneCmd(app))
	cmd.AddCommand(newTodoUndoCmd(app))
	cmd.AddCommand(newTodoRemoveCmd(app))
	cmd.AddCommand(newTodoMoveCmd(app))
	cmd.AddCommand(newTodoProgressCmd(app))
	return cmd
}

func todoSvc(app *app.App) *todo.Service { return todo.NewService(app.TodoStore) }

func newTodoListCmd(app *app.App) *cobra.Command {
	return &cobra.Command{
		Use: "list [list-id]", Short: "Lista tareas (o listas si no se da ID)", Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := ""
			if len(args) > 0 {
				id = args[0]
			}
			return runTodoList(cmd.Context(), app, id)
		},
	}
}
func runTodoList(ctx context.Context, app *app.App, listID string) error {
	svc := todoSvc(app)
	if listID != "" {
		list, err := svc.GetList(ctx, listID)
		if err != nil {
			return fmt.Errorf("lista no encontrada: %w", err)
		}
		printTodoList(list)
		return nil
	}
	lists, err := svc.ListLists(ctx)
	if err != nil {
		return err
	}
	if len(lists) == 0 {
		fmt.Println("No hay listas de tareas. Crea una con 'forgen todo add <contenido>'")
		return nil
	}
	for _, l := range lists {
		d, tot := l.Progress()
		fmt.Printf("%s  %s  (%d/%d %.0f%%)\n", l.ID, l.Name, d, tot, l.ProgressPercent())
	}
	return nil
}
func printTodoList(list *domain.TodoList) {
	d, tot := list.Progress()
	fmt.Printf("%s  (%d/%d %.0f%%)\n", list.Name, d, tot, list.ProgressPercent())
	for _, t := range list.Todos {
		icon := " "
		switch t.Status {
		case domain.TodoStatusDone:
			icon = "✓"
		case domain.TodoStatusInProgress:
			icon = "▸"
		case domain.TodoStatusCancelled:
			icon = "✗"
		}
		fmt.Printf("  %s [%s] %s\n", icon, t.ID[:8], t.Content)
		if t.ActiveForm != "" {
			fmt.Printf("      → %s\n", t.ActiveForm)
		}
	}
}
func newTodoAddCmd(app *app.App) *cobra.Command {
	var listID, activeForm string
	cmd := &cobra.Command{
		Use: "add <contenido>", Short: "Añade una tarea a una lista", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runTodoAdd(cmd.Context(), app, listID, activeForm, args[0]) },
	}
	cmd.Flags().StringVar(&listID, "list", "", "ID de la lista (crea nueva si no existe)")
	cmd.Flags().StringVar(&activeForm, "active", "", "Forma activa (p.ej. 'crear archivo X')")
	return cmd
}
func runTodoAdd(ctx context.Context, app *app.App, listID, activeForm, content string) error {
	svc := todoSvc(app)
	var list *domain.TodoList
	var err error
	if listID != "" {
		list, err = svc.GetList(ctx, listID)
		if err != nil {
			return fmt.Errorf("lista no encontrada: %w", err)
		}
	} else {
		list, err = svc.CreateList(ctx, truncate(content, 40))
		if err != nil {
			return err
		}
		fmt.Printf("Lista creada: %s\n", list.ID)
	}
	_, err = svc.AddTodo(ctx, list.ID, content, activeForm)
	return err
}
func newTodoDoneCmd(app *app.App) *cobra.Command {
	var listID string
	cmd := &cobra.Command{
		Use: "done <todo-id>", Short: "Marca una tarea como completada", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runTodoUpdateStatus(cmd.Context(), app, listID, args[0], domain.TodoStatusDone) },
	}
	cmd.Flags().StringVar(&listID, "list", "", "ID de la lista")
	return cmd
}
func newTodoUndoCmd(app *app.App) *cobra.Command {
	var listID string
	return &cobra.Command{
		Use: "undo <todo-id>", Short: "Desmarca una tarea (vuelve a pendiente)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runTodoUpdateStatus(cmd.Context(), app, listID, args[0], domain.TodoStatusPending) },
	}
}
func newTodoProgressCmd(app *app.App) *cobra.Command {
	return &cobra.Command{
		Use: "progress <list-id>", Short: "Muestra el progreso de una lista", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runTodoProgress(cmd.Context(), app, args[0]) },
	}
}
func runTodoProgress(ctx context.Context, app *app.App, listID string) error {
	svc := todoSvc(app)
	d, tot, pct, err := svc.GetTodoProgress(ctx, listID)
	if err != nil {
		return err
	}
	fmt.Printf("Progreso: %d/%d (%.1f%%)\n", d, tot, pct)
	return nil
}
func runTodoUpdateStatus(ctx context.Context, app *app.App, listID, todoID string, status domain.TodoStatus) error {
	return todoSvc(app).UpdateTodoStatus(ctx, listID, todoID, status)
}
func newTodoRemoveCmd(app *app.App) *cobra.Command {
	var listID string
	return &cobra.Command{
		Use: "remove <todo-id>", Short: "Elimina una tarea", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runTodoRemove(cmd.Context(), app, listID, args[0]) },
	}
}
func runTodoRemove(ctx context.Context, app *app.App, listID, todoID string) error {
	return todoSvc(app).DeleteTodo(ctx, listID, todoID)
}
func newTodoMoveCmd(app *app.App) *cobra.Command {
	var listID string
	return &cobra.Command{
		Use: "move <todo-id> <posición>", Short: "Mueve una tarea a otra posición", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			pos, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("posición inválida: %w", err)
			}
			return runTodoMove(cmd.Context(), app, listID, args[0], pos)
		},
	}
}
func runTodoMove(ctx context.Context, app *app.App, listID, todoID string, pos int) error {
	return todoSvc(app).MoveTodo(ctx, listID, todoID, pos)
}
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
