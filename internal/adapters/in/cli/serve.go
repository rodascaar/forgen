package cli

import (
	"os"

	"github.com/rodascaar/forgen/internal/adapters/in/jsonrpc"
	apppkg "github.com/rodascaar/forgen/internal/app"
	"github.com/spf13/cobra"
)

// newServeCommand construye el servidor JSON-RPC/ACP sobre stdio.
func newServeCommand(app *apppkg.App) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Ejecuta forgen como servidor JSON-RPC sobre stdio (integración con IDEs)",
		Long:  "Lee peticiones JSON-RPC 2.0 (una por línea) y responde. Método principal: agent/run.",
		RunE: func(cmd *cobra.Command, args []string) error {
			server := jsonrpc.NewServer(app, os.Stdin, os.Stdout)
			return server.Serve(cmd.Context())
		},
	}
}
