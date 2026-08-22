package cli

import (
	"context"
	"fmt"

	"github.com/rodascaar/forgen/internal/app"
	tasksvc "github.com/rodascaar/forgen/internal/application/task"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/spf13/cobra"
)

func newTaskCommand(app *app.App) *cobra.Command {
	cmd := &cobra.Command{Use: "task", Short: "Gestiona sub-agentes (tasks)"}
	cmd.AddCommand(newTaskCreateCmd(app))
	cmd.AddCommand(newTaskListCmd(app))
	cmd.AddCommand(newTaskStatusCmd(app))
	cmd.AddCommand(newTaskCancelCmd(app))
	return cmd
}

func taskSvc(app *app.App) *tasksvc.Service {
	return tasksvc.NewService(app.TaskExecutor, app.TaskStore)
}

func newTaskCreateCmd(app *app.App) *cobra.Command {
	var taskType string
	cmd := &cobra.Command{
		Use: "create <nombre> <descripción>", Short: "Crea una tarea/sub-agente", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTaskCreate(cmd.Context(), app, domain.TaskType(taskType), args[0], args[1])
		},
	}
	cmd.Flags().StringVar(&taskType, "type", "build", "Tipo: explore|plan|build|review|research")
	return cmd
}
func runTaskCreate(ctx context.Context, app *app.App, t domain.TaskType, name, desc string) error {
	reg := domain.DefaultSubAgentRegistry()
	cfg, ok := reg.GetAgentConfig(t)
	if !ok {
		return fmt.Errorf("tipo %q no existe", t)
	}
	svc := taskSvc(app)
	task, err := svc.CreateTask(ctx, t, name, desc, cfg)
	if err != nil {
		return err
	}
	fmt.Printf("Tarea creada: %s (%s)\n", task.ID, task.Type)
	return nil
}
func newTaskListCmd(app *app.App) *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "Lista tareas", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return runTaskList(cmd.Context(), app) },
	}
}
func runTaskList(ctx context.Context, app *app.App) error {
	tasks, err := taskSvc(app).ListTasks(ctx, nil)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		fmt.Println("No hay tareas.")
		return nil
	}
	for _, t := range tasks {
		fmt.Printf("%s  %-10s  %-12s  %s\n", t.ID[:8], t.Type, t.Status, t.Name)
	}
	return nil
}
func newTaskStatusCmd(app *app.App) *cobra.Command {
	return &cobra.Command{
		Use: "status <id>", Short: "Estado de una tarea", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runTaskStatus(cmd.Context(), app, args[0]) },
	}
}
func runTaskStatus(ctx context.Context, app *app.App, id string) error {
	s, err := taskSvc(app).GetTaskStatus(ctx, id)
	if err != nil {
		return err
	}
	fmt.Println(s)
	return nil
}
func newTaskCancelCmd(app *app.App) *cobra.Command {
	return &cobra.Command{
		Use: "cancel <id>", Short: "Cancela una tarea", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return taskSvc(app).CancelTask(cmd.Context(), args[0]) },
	}
}
