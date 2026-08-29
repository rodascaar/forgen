package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	apppkg "github.com/rodascaar/forgen/internal/app"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// newProviderCommand construye el árbol de gestión de proveedores y credenciales.
func newProviderCommand(app *apppkg.App) *cobra.Command {
	command := &cobra.Command{
		Use:   "provider",
		Short: "Gestiona proveedores de modelos y sus credenciales",
	}
	command.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Lista proveedores configurados y presets disponibles",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProviderList(cmd.Context(), app)
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "add <proveedor>",
		Short: "Añade un proveedor (preset) y guarda su API key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAuth(cmd.Context(), app, args[0], cmd.InOrStdin(), cmd.OutOrStdout())
		},
	})
	command.AddCommand(&cobra.Command{
		Use:   "remove <proveedor>",
		Short: "Elimina un proveedor y su credencial",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProviderRemove(cmd.Context(), app, args[0])
		},
	})
	return command
}

// newAuthCommand construye el comando de autenticación de proveedores.
func newAuthCommand(app *apppkg.App) *cobra.Command {
	return &cobra.Command{
		Use:   "auth [proveedor]",
		Short: "Configura un proveedor con tu API key y detecta tus modelos",
		Long: `Añade un proveedor conocido, guarda tu API key en el almacén seguro
del sistema y lista los modelos disponibles para tu cuenta. Sin argumento,
te permite elegir un proveedor de la lista de presets.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			return runAuth(cmd.Context(), app, name, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}

// runAuth autentica un proveedor: resuelve preset, pide API key, valida contra
// el proveedor (lista modelos) y guarda credencial + metadata.
func runAuth(ctx context.Context, app *apppkg.App, providerName string, in io.Reader, out io.Writer) error {
	config, err := app.ConfigService.Load(ctx)
	if err != nil {
		return err
	}

	// Resolver el preset o un proveedor ya configurado.
	preset, isPreset := domain.FindProviderPreset(providerName)
	providerConfig, isConfigured := config.FindProvider(providerName)
	if !isPreset && !isConfigured {
		if providerName == "" {
			providerName = promptProviderChoice(in, out)
			preset, isPreset = domain.FindProviderPreset(providerName)
			if !isPreset {
				return fmt.Errorf("proveedor %q desconocido", providerName)
			}
		} else {
			_, _ = fmt.Fprintf(out, "Proveedor %q desconocido. Disponibles: %s\n",
				providerName, strings.Join(domain.PresetNames(), ", "))
			return nil
		}
	}

	// Base del proveedor: preset o metadata existente.
	base := providerConfig
	if isPreset {
		base = preset.ToProviderConfig()
	}

	_, _ = fmt.Fprintf(out, "Proveedor: %s\nBase URL:  %s\n", base.Name, base.BaseURL)
	secret, err := promptSecret(in, out, fmt.Sprintf("API key de %s: ", base.Name))
	if err != nil {
		return err
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return fmt.Errorf("API key vacía")
	}

	// Validar contra el proveedor y detectar modelos disponibles del usuario.
	models, err := app.ValidateProviderKey(ctx, base, secret)
	if err != nil {
		return fmt.Errorf("no se pudo validar la key contra el proveedor: %w", err)
	}
	_, _ = fmt.Fprintf(out, "✓ Autenticación correcta. %d modelos disponibles.\n", len(models))

	// Guardar el secreto en el almacén seguro (nunca en config.yaml).
	if err := app.Credentials.Set(ctx, apppkg.ProviderCredentialKey(base.Name), secret); err != nil {
		return fmt.Errorf("guardar credencial: %w", err)
	}

	// Persistir metadata del proveedor con los modelos detectados.
	providerConfig = base
	providerConfig.Models = models
	defaultModel := ""
	if len(models) > 0 {
		defaultModel = models[0]
	} else if len(providerConfig.Models) > 0 {
		defaultModel = providerConfig.Models[0]
	}
	if err := app.AddProvider(ctx, providerConfig, defaultModel); err != nil {
		return fmt.Errorf("guardar configuración: %w", err)
	}

	_, _ = fmt.Fprintf(out, "\nModelos guardados para %s (%d):\n", providerConfig.Name, len(models))
	for _, model := range models {
		_, _ = fmt.Fprintf(out, "  - %s\n", model)
	}
	_, _ = fmt.Fprintln(out, "\nListo. La API key se guardó en el almacén seguro del sistema.")
	return nil
}

func runProviderList(ctx context.Context, app *apppkg.App) error {
	config, err := app.ConfigService.Load(ctx)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintln(os.Stdout, "Proveedores configurados:")
	if len(config.Providers) == 0 {
		_, _ = fmt.Fprintln(os.Stdout, "  (ninguno) — usa 'forgen auth' para configurar uno.")
	}
	for _, provider := range config.Providers {
		cred := "sin key"
		if _, err := app.Credentials.Get(ctx, apppkg.ProviderCredentialKey(provider.Name)); err == nil {
			cred = "✓ key guardada" //nolint:gosec:G101 // status label, not a credential
		}
		_, _ = fmt.Fprintf(os.Stdout, "  - %s (%s) %s — %s\n", provider.Name, provider.Type, provider.BaseURL, cred)
	}

	_, _ = fmt.Fprintln(os.Stdout, "\nPresets disponibles (forgen auth <nombre>):")
	for _, name := range domain.PresetNames() {
		_, _ = fmt.Fprintf(os.Stdout, "  - %s\n", name)
	}
	return nil
}

func runProviderRemove(ctx context.Context, app *apppkg.App, name string) error {
	config, err := app.ConfigService.Load(ctx)
	if err != nil {
		return err
	}
	updated := make([]domain.ProviderConfig, 0, len(config.Providers))
	found := false
	for _, provider := range config.Providers {
		if provider.Name == name {
			found = true
			continue
		}
		updated = append(updated, provider)
	}
	if !found {
		return fmt.Errorf("proveedor %q no configurado", name)
	}
	config.Providers = updated
	if config.Default.Provider == name {
		config.Default.Provider = ""
		config.Default.Model = ""
	}
	if err := app.Credentials.Delete(ctx, apppkg.ProviderCredentialKey(name)); err != nil {
		return err
	}
	if err := app.ConfigService.Save(ctx, config); err != nil {
		return err
	}
	fmt.Printf("Proveedor %q eliminado.\n", name)
	return nil
}

// promptProviderChoice pide al usuario elegir un preset.
func promptProviderChoice(in io.Reader, out io.Writer) string {
	reader := bufio.NewReader(in)
	_, _ = fmt.Fprintln(out, "Proveedores disponibles:")
	for _, name := range domain.PresetNames() {
		_, _ = fmt.Fprintf(out, "  - %s\n", name)
	}
	_, _ = fmt.Fprint(out, "Nombre del proveedor: ")
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

// promptSecret lee un secreto sin eco si el input es una terminal; si no,
// lo lee de una línea (modo piped/CI).
func promptSecret(in io.Reader, out io.Writer, label string) (string, error) {
	if in == os.Stdin && term.IsTerminal(int(os.Stdin.Fd())) {
		_, _ = fmt.Fprint(out, label)
		secret, err := term.ReadPassword(int(os.Stdin.Fd()))
		_, _ = fmt.Fprintln(out)
		if err != nil {
			return "", err
		}
		return string(secret), nil
	}
	_, _ = fmt.Fprint(out, label)
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSuffix(line, "\n"), nil
}
