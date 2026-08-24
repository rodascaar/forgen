package cli

import (
	"fmt"

	apppkg "github.com/rodascaar/forgen/internal/app"
	"github.com/spf13/cobra"
)

// newCheckpointCommand agrupa los comandos de rollback interno: undo y list.
func newCheckpointCommand(app *apppkg.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checkpoints",
		Short: "Snapshots del workspace para revertir iteraciones (rollback interno)",
	}
	cmd.AddCommand(newUndoCommand(app))
	cmd.AddCommand(newCheckpointListCommand(app))
	return cmd
}

func newUndoCommand(app *apppkg.App) *cobra.Command {
	return &cobra.Command{
		Use:   "undo [sessionID]",
		Short: "Revierte la última iteración del agente al snapshot previo",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := ""
			if len(args) > 0 {
				sessionID = args[0]
			}
			ok, err := app.UndoLast(cmd.Context(), sessionID)
			if err != nil {
				return err
			}
			if ok {
				fmt.Println("✓ Última iteración revertida.")
			} else {
				fmt.Println("No hay checkpoint para revertir.")
			}
			return nil
		},
	}
}

func newCheckpointListCommand(app *apppkg.App) *cobra.Command {
	var sessionID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Lista los checkpoints de una sesión",
		RunE: func(cmd *cobra.Command, _ []string) error {
			list, err := app.ListCheckpoints(cmd.Context(), sessionID, 20)
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Println("No hay checkpoints.")
				return nil
			}
			for _, cp := range list {
				fmt.Printf("%s  sesión=%s  archivos=%d  bytes=%d  %s\n",
					cp.ID, cp.SessionID, cp.FileCount, cp.TotalBytes, cp.CreatedAt.Format("2006-01-02 15:04:05"))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", "Sesión (por defecto todas)")
	return cmd
}
