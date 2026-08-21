package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	apppkg "github.com/rodascaar/forgen/internal/app"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/spf13/cobra"
)

// newInitCommand construye el wizard de configuración inicial.
func newInitCommand(app *apppkg.App) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Configura forgen por primera vez (proveedores y modelo)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd.Context(), app, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}

func runInit(ctx context.Context, app *apppkg.App, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	config, err := app.ConfigService.Load(ctx)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(out, "Bienvenido a forgen.")
	_, _ = fmt.Fprintf(out, "Config existente en: %s\n", app.Paths.ConfigFile)

	if len(config.Providers) > 0 {
		_, _ = fmt.Fprintln(out, "\nProveedores configurados:")
		for _, provider := range config.Providers {
			_, _ = fmt.Fprintf(out, "  - %s (%s) modelos: %s\n", provider.Name, provider.Type, strings.Join(provider.Models, ", "))
		}
		answer := ask(reader, out, "\n¿Añadir un nuevo proveedor? [y/N] ")
		if !strings.EqualFold(answer, "y") {
			return saveConfigIfNeeded(ctx, app, config)
		}
	}

	provider, err := promptProvider(reader, out)
	if err != nil {
		return err
	}
	config.Providers = append(config.Providers, provider)

	_, _ = fmt.Fprintf(out, "\nModelo por defecto de %s: ", provider.Name)
	model := strings.TrimSpace(readLine(reader))
	if model == "" && len(provider.Models) > 0 {
		model = provider.Models[0]
	}
	if model != "" {
		config.Default.Provider = provider.Name
		config.Default.Model = model
	}

	answer := ask(reader, out, "\n¿Permisos automáticos para herramientas rutinarias? [Y/n] ")
	if strings.EqualFold(answer, "n") {
		config.Permissions.Mode = "on_request"
	} else {
		config.Permissions.Mode = "auto"
	}

	return saveConfigIfNeeded(ctx, app, config)
}

func promptProvider(reader *bufio.Reader, out io.Writer) (domain.ProviderConfig, error) {
	_, _ = fmt.Fprintln(out, "\nNuevo proveedor LLM.")
	name := ask(reader, out, "Nombre (ej: openai, anthropic, local): ")
	if name == "" {
		name = "openai"
	}

	_, _ = fmt.Fprintln(out, "Tipos soportados: openai_compatible | anthropic | kimchi")
	providerType := ask(reader, out, "Tipo [openai_compatible]: ")
	if providerType == "" {
		providerType = "openai_compatible"
	}
	if !isValidProviderType(providerType) {
		return domain.ProviderConfig{}, fmt.Errorf("tipo de proveedor inválido: %q", providerType)
	}

	baseURL := ask(reader, out, "Base URL (ej: https://api.openai.com/v1): ")
	apiKeyEnv := ask(reader, out, "Variable de entorno de la API key (ej: OPENAI_API_KEY): ")
	modelsRaw := ask(reader, out, "Modelos separados por coma (ej: gpt-5, gpt-5-mini): ")

	models := splitModels(modelsRaw)
	if len(models) == 0 {
		return domain.ProviderConfig{}, fmt.Errorf("debes indicar al menos un modelo")
	}

	return domain.ProviderConfig{
		Name:      name,
		Type:      domain.ProviderType(providerType),
		BaseURL:   baseURL,
		APIKeyEnv: apiKeyEnv,
		Models:    models,
	}, nil
}

func saveConfigIfNeeded(ctx context.Context, app *apppkg.App, config domain.AppConfig) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("configuración inválida: %w", err)
	}
	if err := app.ConfigService.Save(ctx, config); err != nil {
		return err
	}
	fmt.Printf("\nConfiguración guardada en %s\n", app.Paths.ConfigFile)
	return nil
}

func isValidProviderType(providerType string) bool {
	switch domain.ProviderType(providerType) {
	case domain.ProviderTypeOpenAICompatible, domain.ProviderTypeAnthropic, domain.ProviderTypeKimchi:
		return true
	}
	return false
}

func splitModels(raw string) []string {
	parts := strings.Split(raw, ",")
	models := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			models = append(models, trimmed)
		}
	}
	return models
}

func ask(reader *bufio.Reader, out io.Writer, question string) string {
	_, _ = fmt.Fprint(out, question)
	return strings.TrimSpace(readLine(reader))
}

func readLine(reader *bufio.Reader) string {
	line, err := reader.ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(line, "\n")
}
