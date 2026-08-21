package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	apppkg "github.com/rodascaar/forgen/internal/app"
	"github.com/rodascaar/forgen/internal/application/upgrade"
	"github.com/spf13/cobra"
)

// newUpgradeCommand construye el comando de auto-actualización desde GitHub.
func newUpgradeCommand() *cobra.Command {
	var check, yes bool
	command := &cobra.Command{
		Use:   "upgrade",
		Short: "Actualiza forgen a la última versión (GitHub releases)",
		RunE: func(cmd *cobra.Command, args []string) error {
			service := upgrade.New()
			service.CurrentVersion = func() string { return apppkg.Version }

			latest, hasUpdate, err := service.Check(cmd.Context())
			if err != nil {
				return fmt.Errorf("no se pudo consultar la última versión: %w", err)
			}
			if !hasUpdate {
				fmt.Printf("Ya tienes la última versión (%s).\n", apppkg.Version)
				return nil
			}

			fmt.Printf("Versión actual: %s\nÚltima versión: %s\n", apppkg.Version, latest.Tag)
			if check {
				fmt.Println("Ejecuta 'forgen upgrade' para actualizar.")
				return nil
			}
			if !yes && !confirm(fmt.Sprintf("¿Actualizar a %s? [y/N] ", latest.Tag)) {
				fmt.Println("Actualización cancelada.")
				return nil
			}
			if err := service.Apply(cmd.Context(), latest.Tag); err != nil {
				return err
			}
			fmt.Printf("forgen actualizado a %s. Reinicia la sesión de la TUI si estaba abierta.\n", latest.Tag)
			return nil
		},
	}
	command.Flags().BoolVar(&check, "check", false, "Solo comprueba si hay una versión nueva")
	command.Flags().BoolVarP(&yes, "yes", "y", false, "Confirma la actualización sin preguntar")
	return command
}

// newVersionCommand construye el comando que muestra la versión.
func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Muestra la versión de forgen",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("forgen %s (commit %s)\n", apppkg.Version, apppkg.Commit)
		},
	}
}

// confirm pide una confirmación sí/no en la terminal.
func confirm(prompt string) bool {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.EqualFold(strings.TrimSpace(line), "y")
}
