package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	apppkg "github.com/rodascaar/forgen/internal/app"
	"github.com/rodascaar/forgen/internal/core/domain"
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
		Use:   "new",
		Short: "Crea una sesión nueva",
		RunE: func(cmd *cobra.Command, args []string) error {
			workspace, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolver workspace: %w", err)
			}
			appConfig, err := app.LoadConfig(cmd.Context())
			if err != nil {
				appConfig = domain.DefaultAppConfig()
			}
			model := resolveModelFromConfig(appConfig)
			agentDef, err := app.SelectedAgent(appConfig, appConfig.Agent)
			if err != nil {
				return err
			}
			session, err := app.SessionService.Create(cmd.Context(), workspace, model, agentDef.Name)
			if err != nil {
				return err
			}
			fmt.Printf("Sesión creada: %s\n", session.ID)
			fmt.Println("Usa 'forgen ask --session " + session.ID + " \"tu prompt\"' para empezar.")
			return nil
		},
	})
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
			_, _ = os.Stdout.Write(data)
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

// resolveModelFromConfig deriva un modelo (provider/id) desde la config efectiva.
func resolveModelFromConfig(cfg domain.AppConfig) domain.Model {
	provider := cfg.Default.Provider
	id := cfg.Default.Model
	if i := strings.IndexByte(id, '/'); i >= 0 {
		if id[:i] != "" {
			provider = id[:i]
		}
		id = id[i+1:]
	}
	return domain.Model{Provider: provider, ID: id}
}
