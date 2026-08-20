package cli

import (
	"context"
	"fmt"

	apppkg "github.com/forgen/forgen/internal/app"
	"github.com/forgen/forgen/internal/application/usage"
	"github.com/spf13/cobra"
)

// newUsageCommand construye el comando de consulta de uso/costos.
func newUsageCommand(app *apppkg.App) *cobra.Command {
	return &cobra.Command{
		Use:   "usage",
		Short: "Muestra el consumo de tokens por modelo",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUsage(cmd.Context(), app)
		},
	}
}

func runUsage(ctx context.Context, app *apppkg.App) error {
	records, err := app.UsageService.List(ctx, 0)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		fmt.Println("No hay registros de uso todavía.")
		return nil
	}
	summaries := usage.Summarize(records)
	fmt.Printf("%-40s %-10s %-12s %-12s\n", "MODELO", "REQUESTS", "INPUT TOKENS", "OUTPUT TOKENS")
	for _, summary := range summaries {
		fmt.Printf("%-40s %-10d %-12d %-12d\n",
			summary.Model, summary.Requests, summary.InputTokens, summary.OutputTokens)
	}
	return nil
}
