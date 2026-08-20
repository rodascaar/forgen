package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	apppkg "github.com/forgen/forgen/internal/app"
	"github.com/forgen/forgen/internal/core/domain"
	"github.com/spf13/cobra"
)

// newFermentCommand construye el grupo de comandos de Ferment.
func newFermentCommand(app *apppkg.App) *cobra.Command {
	command := &cobra.Command{
		Use:   "ferment",
		Short: "Gestión de proyectos multi-sesión (Ferment)",
	}
	command.AddCommand(&cobra.Command{
		Use:   "new [nombre]",
		Short: "Crea un nuevo ferment (proyecto)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFermentNew(cmd.Context(), app, args[0])
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Lista los ferments",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFermentList(cmd.Context(), app)
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "switch [id]",
		Short: "Establece el ferment activo",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFermentSwitch(app, args[0])
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "progress [id]",
		Short: "Muestra el progreso de fases y pasos",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFermentProgress(cmd.Context(), app, args[0])
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "export [id]",
		Short: "Exporta el ferment a JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFermentExport(cmd.Context(), app, args[0])
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "pause [id]",
		Short: "Pausa el ferment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFermentPause(cmd.Context(), app, args[0])
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "resume [id]",
		Short: "Reanuda el ferment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFermentResume(cmd.Context(), app, args[0])
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "delete [id]",
		Short: "Elimina un ferment permanentemente",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFermentDelete(cmd.Context(), app, args[0])
		},
	})
	return command
}

func runFermentNew(ctx context.Context, app *apppkg.App, name string) error {
	goal := ""
	fmt.Printf("Objetivo del ferment %q (opcional, Enter para omitir): ", name)
	goal = strings.TrimSpace(readLineFromStdin())
	ferment, err := app.FermentService.Create(ctx, name, goal)
	if err != nil {
		return err
	}
	app.ActiveFermentID = ferment.ID
	fmt.Printf("Ferment %q creado (id %s, estado draft).\n", name, ferment.ID)
	fmt.Println("Usa 'forgen ferment switch <id>' para retomarlo en otra sesión.")
	return nil
}

func runFermentList(ctx context.Context, app *apppkg.App) error {
	ferments, err := app.FermentService.List(ctx)
	if err != nil {
		return err
	}
	if len(ferments) == 0 {
		fmt.Println("No hay ferments. Crea uno con 'forgen ferment new <nombre>'.")
		return nil
	}
	fmt.Printf("%-38s %-10s %-8s %s\n", "ID", "ESTADO", "PROGRESO", "NOMBRE")
	for _, ferment := range ferments {
		progress := fmt.Sprintf("%d/%d", ferment.CompletedSteps(), ferment.TotalSteps())
		fmt.Printf("%-38s %-10s %-8s %s\n",
			ferment.ID[:min(len(ferment.ID), 38)],
			ferment.Status,
			progress,
			ferment.Name)
	}
	return nil
}

func runFermentSwitch(app *apppkg.App, id string) error {
	if _, err := app.FermentService.Load(context.Background(), id); err != nil {
		return err
	}
	app.ActiveFermentID = id
	fmt.Printf("Ferment activo: %s\n", id)
	return nil
}

func runFermentProgress(ctx context.Context, app *apppkg.App, id string) error {
	ferment, err := app.FermentService.Load(ctx, id)
	if err != nil {
		return err
	}
	fmt.Printf("%s [%s] — %d/%d pasos\n\n", ferment.Name, ferment.Status, ferment.CompletedSteps(), ferment.TotalSteps())
	if ferment.Goal != "" {
		fmt.Printf("Objetivo: %s\n", ferment.Goal)
	}
	for phaseIndex, phase := range ferment.Phases {
		fmt.Printf("\nFase %d: %s [%s]\n", phaseIndex+1, phase.Name, phase.Status)
		for stepIndex, step := range phase.Steps {
			marker := "  "
			if step.Status == domain.StepStatusCompleted {
				marker = "x "
			} else if step.Status == domain.StepStatusActive {
				marker = "> "
			}
			fmt.Printf("  %s%d.%d %s\n", marker, phaseIndex+1, stepIndex+1, step.Task)
		}
	}
	return nil
}

func runFermentExport(ctx context.Context, app *apppkg.App, id string) error {
	ferment, err := app.FermentService.Load(ctx, id)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(ferment, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func runFermentPause(ctx context.Context, app *apppkg.App, id string) error {
	ferment, err := app.FermentService.Pause(ctx, id)
	if err != nil {
		return err
	}
	fmt.Printf("Ferment %q pausado (estado %s).\n", ferment.Name, ferment.Status)
	return nil
}

func runFermentResume(ctx context.Context, app *apppkg.App, id string) error {
	ferment, err := app.FermentService.Resume(ctx, id)
	if err != nil {
		return err
	}
	fmt.Printf("Ferment %q reanudado (estado %s).\n", ferment.Name, ferment.Status)
	return nil
}

func runFermentDelete(ctx context.Context, app *apppkg.App, id string) error {
	if err := app.FermentService.Delete(ctx, id); err != nil {
		return err
	}
	if app.ActiveFermentID == id {
		app.ActiveFermentID = ""
	}
	fmt.Printf("Ferment %s eliminado.\n", id)
	return nil
}

func readLineFromStdin() string {
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return ""
	}
	return strings.TrimSpace(scanner.Text())
}
