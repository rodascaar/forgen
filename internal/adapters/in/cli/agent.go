package cli

import (
	"context"
	"fmt"
	"strings"

	apppkg "github.com/rodascaar/forgen/internal/app"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/spf13/cobra"
)

// newAgentCommand construye el comando de selección de agente.
func newAgentCommand(app *apppkg.App) *cobra.Command {
	command := &cobra.Command{
		Use:   "agent",
		Short: "Lista los agentes disponibles",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runListAgents(cmd.Context(), app)
		},
	}
	command.AddCommand(&cobra.Command{
		Use:   "use [build|plan]",
		Short: "Establece el agente por defecto",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUseAgent(cmd.Context(), app, args[0])
		},
	})
	return command
}

func runListAgents(ctx context.Context, app *apppkg.App) error {
	config, err := app.ConfigService.Load(ctx)
	if err != nil {
		return err
	}
	agents := domain.BuiltinAgents()
	for _, agentDef := range agents {
		marker := " "
		if agentDef.Name == config.Agent {
			marker = "*"
		}
		fmt.Printf("%s %-6s %s\n", marker, agentDef.Name, agentDef.Description)
	}
	return nil
}

func runUseAgent(ctx context.Context, app *apppkg.App, name string) error {
	config, err := app.ConfigService.Load(ctx)
	if err != nil {
		return err
	}
	name = strings.ToLower(name)
	if _, ok := domain.FindAgent(domain.BuiltinAgents(), name); !ok {
		return fmt.Errorf("agente %q inválido (build | plan)", name)
	}
	config.Agent = name
	if err := app.ConfigService.Save(ctx, config); err != nil {
		return err
	}
	fmt.Printf("Agente por defecto: %s\n", name)
	return nil
}
