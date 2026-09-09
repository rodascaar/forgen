package cli

import (
	"fmt"
	"os"
	"strings"

	apppkg "github.com/rodascaar/forgen/internal/app"
	loclexec "github.com/rodascaar/forgen/internal/adapters/out/exec"
	"github.com/rodascaar/forgen/internal/adapters/out/sandbox"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/spf13/cobra"
)

// newDebugCommand agrupa utilidades de diagnóstico (estilo `codex debug`).
func newDebugCommand(app *apppkg.App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Utilidades de diagnóstico (sandbox, perfiles)",
	}
	cmd.AddCommand(newDebugSandboxCommand(app))
	return cmd
}

// newDebugSandboxCommand ejecuta un comando bajo la política sandbox efectiva
// (estilo `codex debug seatbelt -- <cmd>`): muestra backend, perfil y resultado.
func newDebugSandboxCommand(app *apppkg.App) *cobra.Command {
	var showProfile bool
	cmd := &cobra.Command{
		Use:   "sandbox -- <comando>",
		Short: "Prueba la política sandbox ejecutando un comando confinado",
		Long: `Ejecuta <comando> bajo la política sandbox efectiva (execution.sandbox/mode/network)
y muestra backend, perfil y resultado. Sirve para verificar que una herramienta
pasará el enforcement antes de usarla en una sesión real.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("uso: forgen debug sandbox -- <comando>")
			}
			command := strings.Join(args, " ")
			ctx := cmd.Context()
			workspace, _ := os.Getwd()
			appConfig, err := app.LoadConfig(ctx)
			if err != nil {
				appConfig = domain.DefaultAppConfig()
			}
			local := loclexec.New(workspace)
			native := sandbox.NewNativeExecutor(workspace,
				domain.ParseSandboxMode(appConfig.Execution.Mode),
				appConfig.Execution.Network,
				appConfig.Execution.ExtraWritable,
				local, app.Logger)
			fmt.Printf("policy: %s\n", native.Describe())
			if showProfile {
				fmt.Printf("--- profile ---\n%s\n--- end profile ---\n", native.ProfileText())
			}
			result, err := native.Execute(ctx, command, workspace, nil)
			fmt.Printf("exit=%d\n--- stdout ---\n%s\n--- stderr ---\n%s\n",
				result.ExitCode, result.Stdout, result.Stderr)
			return err
		},
	}
	cmd.Flags().BoolVar(&showProfile, "profile", false, "Muestra el perfil SBPL efectivo")
	return cmd
}
