package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	apppkg "github.com/rodascaar/forgen/internal/app"
	"github.com/spf13/cobra"
)

// newConfigCommand construye el comando de inspección de configuración.
func newConfigCommand(app *apppkg.App) *cobra.Command {
	command := &cobra.Command{
		Use:   "config",
		Short: "Muestra la configuración efectiva",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runShowConfig(cmd.Context(), app)
		},
	}
	command.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Muestra la ruta del archivo de configuración",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println(app.Paths.ConfigFile)
			return nil
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "set",
		Short: "Establece el proveedor/modelo/agente por defecto",
		Example: "  forgen config set provider anthropic\n" +
			"  forgen config set model claude-sonnet-4-5\n" +
			"  forgen config set agent plan",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetConfig(cmd.Context(), app, args[0], args[1])
		},
	})
	return command
}

func runShowConfig(ctx context.Context, app *apppkg.App) error {
	data, err := os.ReadFile(app.Paths.ConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No existe archivo de configuración. Usa 'forgen init' para crearlo.")
			return nil
		}
		return err
	}
	fmt.Print(string(data))
	return nil
}

func runSetConfig(ctx context.Context, app *apppkg.App, key, value string) error {
	config, err := app.ConfigService.Load(ctx)
	if err != nil {
		return err
	}
	switch key {
	case "provider":
		if _, ok := config.FindProvider(value); !ok {
			return fmt.Errorf("proveedor %q no configurado (usa 'forgen init')", value)
		}
		config.Default.Provider = value
	case "model":
		config.Default.Model = value
	case "agent":
		value = strings.ToLower(value)
		if value != "build" && value != "plan" {
			return fmt.Errorf("agente %q inválido (build | plan)", value)
		}
		config.Agent = value
	case "search.provider":
		value = strings.ToLower(value)
		if value != "" && value != "brave" {
			return fmt.Errorf("search.provider %q inválido (brave | vacío para deshabilitar)", value)
		}
		config.Search.Provider = value
		if value == "brave" && config.Search.APIKeyEnv == "" {
			config.Search.APIKeyEnv = "BRAVE_API_KEY"
		}
	case "search.apikey":
		if value == "" {
			return fmt.Errorf("search.apikey no puede estar vacío")
		}
		if app.Credentials == nil {
			return fmt.Errorf("no hay almacén de credenciales disponible")
		}
		config.Search.Provider = "brave"
		config.Search.APIKeyEnv = "BRAVE_API_KEY"
		if err := app.Credentials.Set(ctx, apppkg.SearchCredentialKey("brave"), value); err != nil {
			return fmt.Errorf("no se pudo guardar la API key de búsqueda: %w", err)
		}
		value = "(guardada en el almacén seguro de credenciales)"
	case "orchestration.auto":
		auto := strings.ToLower(value)
		if auto != "true" && auto != "false" && auto != "1" && auto != "0" {
			return fmt.Errorf("orchestration.auto %q inválido (true | false)", value)
		}
		config.Orchestration.Auto = auto == "true" || auto == "1"
	case "orchestration.models":
		config.Orchestration.Auto = true
		value = strings.TrimSpace(value)
		if value == "" {
			config.Orchestration.Pool = nil
		} else {
			parts := strings.Split(value, ",")
			pool := make([]string, 0, len(parts))
			for _, p := range parts {
				if p = strings.TrimSpace(p); p != "" {
					pool = append(pool, p)
				}
			}
			config.Orchestration.Pool = pool
		}
	default:
		return fmt.Errorf("clave desconocida %q (usa provider, model, agent, search.provider, search.apikey, orchestration.auto o orchestration.models)", key)
	}
	if err := app.ConfigService.Save(ctx, config); err != nil {
		return err
	}
	fmt.Printf("Configuración actualizada: %s = %s\n", key, value)
	return nil
}
