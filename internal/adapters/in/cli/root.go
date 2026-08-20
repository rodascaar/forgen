// Package cli contiene los adaptadores de entrada por línea de comandos.
package cli

import (
	"log/slog"
	"os"

	apppkg "github.com/forgen/forgen/internal/app"
	"github.com/spf13/cobra"
)

// newApp construye el composition root con logging a stderr.
func newApp() (*apppkg.App, error) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return apppkg.NewApp(logger)
}

// NewRootCommand construye el árbol de comandos de forgen.
func NewRootCommand() (*cobra.Command, error) {
	app, err := newApp()
	if err != nil {
		return nil, err
	}

	root := &cobra.Command{
		Use:   "forgen",
		Short: "Agente de código agnóstico a lenguaje y proveedor",
		Long: `forgen es un agente de desarrollo que corre en tu terminal.
Trabaja con cualquier lenguaje y proveedor LLM (OpenAI-compatible, Anthropic, Kimchi).
Ejecuta 'forgen' sin argumentos para la interfaz interactiva.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInteractive(app, cmd)
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			app.Close()
			return nil
		},
	}

	root.AddCommand(newAskCommand(app))
	root.AddCommand(newInitCommand(app))
	root.AddCommand(newDoctorCommand(app))
	root.AddCommand(newSessionsCommand(app))
	root.AddCommand(newConfigCommand(app))
	root.AddCommand(newAgentCommand(app))
	root.AddCommand(newFermentCommand(app))
	root.AddCommand(newUsageCommand(app))
	root.AddCommand(newTraceCommand(app))
	root.AddCommand(newAuditCommand(app))
	root.AddCommand(newServeCommand(app))
	return root, nil
}
