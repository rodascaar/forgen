package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	apppkg "github.com/forgen/forgen/internal/app"
	"github.com/spf13/cobra"
)

// newSessionsCommand construye el comando de gestión de sesiones.
func newSessionsCommand(app *apppkg.App) *cobra.Command {
	command := &cobra.Command{
		Use:   "sessions",
		Short: "Lista las sesiones guardadas",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runListSessions(cmd.Context(), app)
		},
	}
	command.AddCommand(&cobra.Command{
		Use:   "resume [id]",
		Short: "Resume una sesión existente",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runResumeSession(cmd.Context(), app, args[0])
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "delete [id]",
		Short: "Elimina una sesión",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeleteSession(cmd.Context(), app, args[0])
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "export [id]",
		Short: "Exporta una sesión a stdout (JSONL portable)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := app.SessionService.Export(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			os.Stdout.Write(data)
			return nil
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "import",
		Short: "Importa una sesión desde stdin (JSONL portable)",
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return err
			}
			session, err := app.SessionService.Import(cmd.Context(), data)
			if err != nil {
				return err
			}
			fmt.Printf("Sesión importada: %s\n", session.ID)
			return nil
		},
	})
	return command
}

func runListSessions(ctx context.Context, app *apppkg.App) error {
	sessions, err := app.SessionService.List(ctx, 20)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Println("No hay sesiones guardadas.")
		return nil
	}
	fmt.Printf("%-38s %-14s %-24s %s\n", "ID", "AGENTE", "MODELO", "RESUMEN")
	for _, session := range sessions {
		fmt.Printf("%-38s %-14s %-24s %s\n",
			session.ID[:min(len(session.ID), 38)],
			session.Agent,
			session.Model.Key(),
			session.Summary())
	}
	return nil
}

func runResumeSession(ctx context.Context, app *apppkg.App, sessionID string) error {
	session, err := app.SessionService.Resume(ctx, sessionID)
	if err != nil {
		return err
	}
	fmt.Printf("Sesión %s resumida (%d mensajes, %s).\n",
		session.ID, len(session.Messages), session.Model.Key())
	fmt.Println("Usa 'forgen ask --session " + session.ID + " \"tu prompt\"' para continuar.")
	return nil
}

func runDeleteSession(ctx context.Context, app *apppkg.App, sessionID string) error {
	if err := app.SessionService.Delete(ctx, sessionID); err != nil {
		return err
	}
	fmt.Printf("Sesión %s eliminada.\n", sessionID)
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
