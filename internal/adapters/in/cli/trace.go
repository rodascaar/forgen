package cli

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	apppkg "github.com/forgen/forgen/internal/app"
	"github.com/forgen/forgen/internal/application/usage"
	"github.com/spf13/cobra"
)

// newTraceCommand construye el comando de diagnóstico exportable.
func newTraceCommand(app *apppkg.App) *cobra.Command {
	return &cobra.Command{
		Use:   "trace",
		Short: "Genera un diagnóstico en markdown (para compartir y depurar)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Print(buildTrace(cmd.Context(), app))
			return nil
		},
	}
}

// buildTrace construye el reporte de diagnóstico markdown, redactando secretos.
func buildTrace(ctx context.Context, app *apppkg.App) string {
	var report strings.Builder

	report.WriteString("# forgen trace\n\n")
	fmt.Fprintf(&report, "- Versión: %s (commit %s)\n", apppkg.Version, apppkg.Commit)
	fmt.Fprintf(&report, "- SO: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&report, "- Go: %s\n", runtime.Version())
	fmt.Fprintf(&report, "- Workspace: %s\n", currentDir())

	// Configuración (sin secretos).
	config, err := app.ConfigService.Load(ctx)
	if err != nil {
		fmt.Fprintf(&report, "- Configuración: ERROR %v\n", err)
	} else {
		report.WriteString("\n## Configuración\n\n")
		fmt.Fprintf(&report, "- Agente: %s\n", config.Agent)
		fmt.Fprintf(&report, "- Proveedor/modelo: %s/%s\n", config.Default.Provider, config.Default.Model)
		fmt.Fprintf(&report, "- Modo de permisos: %s\n", config.Permissions.Mode)
		fmt.Fprintf(&report, "- Máx iteraciones: %d\n", config.MaxIterations)
		report.WriteString("- Proveedores:\n")
		for _, provider := range config.Providers {
			keyState := "no definida"
			if provider.APIKeyEnv != "" {
				if os.Getenv(provider.APIKeyEnv) != "" {
					keyState = "definida ✓"
				} else {
					keyState = "definida ✗ (env vacía)"
				}
			}
			fmt.Fprintf(&report, "  - %s (%s) key:%s\n", provider.Name, provider.Type, keyState)
		}
	}

	// Sesiones.
	sessions, err := app.SessionService.List(ctx, 10)
	if err == nil && len(sessions) > 0 {
		report.WriteString("\n## Sesiones recientes\n\n")
		for _, session := range sessions {
			fmt.Fprintf(&report, "- %s · %s · %s\n", session.ID[:min(len(session.ID), 8)], session.Model.Key(), session.Summary())
		}
	}

	// Uso reciente.
	records, err := app.UsageService.List(ctx, 0)
	if err == nil && len(records) > 0 {
		report.WriteString("\n## Uso de tokens\n\n")
		for _, summary := range usage.Summarize(records) {
			fmt.Fprintf(&report, "- %s: %d requests, %d in / %d out\n", summary.Model, summary.Requests, summary.InputTokens, summary.OutputTokens)
		}
	}

	// Entorno relevante.
	report.WriteString("\n## Entorno\n\n")
	for _, variable := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "KIMCHI_API_KEY", "BRAVE_API_KEY", "HTTP_PROXY", "HTTPS_PROXY"} {
		state := "no definida"
		if os.Getenv(variable) != "" {
			state = "definida"
		}
		fmt.Fprintf(&report, "- %s: %s\n", variable, state)
	}

	report.WriteString("\n> Revisa y redacta este reporte antes de compartirlo.\n")
	return report.String()
}

func currentDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return "(desconocido)"
	}
	return dir
}
