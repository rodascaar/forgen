package cli

import (
	"context"
	"fmt"
	"sort"

	apppkg "github.com/forgen/forgen/internal/app"
	"github.com/forgen/forgen/internal/core/domain"
	"github.com/spf13/cobra"
)

// newAuditCommand construye el comando de auditoría de una sesión.
func newAuditCommand(app *apppkg.App) *cobra.Command {
	return &cobra.Command{
		Use:   "audit [session-id]",
		Short: "Audita una sesión (mensajes, tools, uso)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAudit(cmd.Context(), app, args[0])
		},
	}
}

func runAudit(ctx context.Context, app *apppkg.App, sessionID string) error {
	session, err := app.SessionService.Resume(ctx, sessionID)
	if err != nil {
		return err
	}

	toolCalls := map[string]int{}
	userMessages := 0
	assistantMessages := 0
	toolMessages := 0
	for _, message := range session.Messages {
		switch message.Role {
		case domain.RoleUser:
			userMessages++
		case domain.RoleAssistant:
			assistantMessages++
			for _, call := range message.ToolCalls() {
				toolCalls[call.Name]++
			}
		case domain.RoleTool:
			toolMessages++
		}
	}

	fmt.Printf("Auditoría de sesión %s\n\n", session.ID)
	fmt.Printf("  Agente:       %s\n", session.Agent)
	fmt.Printf("  Modelo:       %s\n", session.Model.Key())
	fmt.Printf("  Workspace:    %s\n", session.Workspace)
	fmt.Printf("  Mensajes:     %d (user=%d, assistant=%d, tool=%d)\n",
		len(session.Messages), userMessages, assistantMessages, toolMessages)
	fmt.Printf("  Tool calls:   %d\n", totalCalls(toolCalls))

	if len(toolCalls) > 0 {
		fmt.Println("\n  Herramientas usadas:")
		for _, name := range sortedToolNames(toolCalls) {
			fmt.Printf("    - %s: %d\n", name, toolCalls[name])
		}
	}
	return nil
}

func totalCalls(toolCalls map[string]int) int {
	total := 0
	for _, count := range toolCalls {
		total += count
	}
	return total
}

func sortedToolNames(toolCalls map[string]int) []string {
	names := make([]string, 0, len(toolCalls))
	for name := range toolCalls {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
