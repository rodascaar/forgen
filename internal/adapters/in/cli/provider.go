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
	addCmd := &cobra.Command{
		Use:   "add [proveedor]",
		Short: "Añade un proveedor (preset o custom OpenAI-compatible) y guarda su API key",
		Long: `Añade un proveedor. Para presets: forgen provider add openai
Para custom OpenAI-compatible (llama.cpp, vLLM, LM Studio):
  forgen provider add custom --base-url http://localhost:8080/v1
  forgen provider add custom --base-url http://localhost:8080/v1 --model my-model --no-auth
  forgen provider add --base-url http://localhost:8080/v1 --name mi-local`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			return runProviderAdd(cmd.Context(), app, cmd, name, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	addCmd.Flags().String("base-url", "", "URL base del endpoint OpenAI-compatible (ej: http://localhost:8080/v1)")
	addCmd.Flags().String("models", "", "Modelos separados por coma (si se omite, se auto-detectan vía GET /v1/models)")
	addCmd.Flags().String("api-key-env", "", "Variable de entorno para la API key (vacío para sin auth)")
	addCmd.Flags().Bool("no-auth", false, "No requiere API key (para endpoints locales como llama.cpp)")
	addCmd.Flags().String("name", "", "Nombre del proveedor (alias de [proveedor])")
	command.AddCommand(addCmd)
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

// runProviderAdd maneja tanto presets como custom OpenAI-compatible.
// Si se pasan flags --base-url / --name / --models / --no-auth, construye un
// proveedor custom aunque el nombre coincida con un preset (permite override).
func runProviderAdd(ctx context.Context, app *apppkg.App, cmd *cobra.Command, providerName string, in io.Reader, out io.Writer) error {
	baseURLOverride, _ := cmd.Flags().GetString("base-url")
	modelsFlag, _ := cmd.Flags().GetString("models")
	noAuth, _ := cmd.Flags().GetBool("no-auth")
	nameOverride, _ := cmd.Flags().GetString("name")

	effectiveName := providerName
	if nameOverride != "" {
		effectiveName = nameOverride
	}
	if effectiveName == "" && baseURLOverride != "" {
		effectiveName = "custom"
	}
	// Si hay flags custom, delegar a flujo custom aunque effectiveName sea preset.
	hasCustomFlags := baseURLOverride != "" || cmd.Flags().Changed("models") || cmd.Flags().Changed("api-key-env") || noAuth || nameOverride != ""
	if hasCustomFlags {
		return runProviderAddCustom(ctx, app, effectiveName, baseURLOverride, modelsFlag, noAuth, cmd, in, out)
	}
	// Sin flags custom, comportamiento legacy (preset).
	return runAuth(ctx, app, effectiveName, in, out)
}

func runProviderAddCustom(ctx context.Context, app *apppkg.App, effectiveName, baseURLOverride, modelsFlag string, noAuth bool, cmd *cobra.Command, in io.Reader, out io.Writer) error {
	if effectiveName == "" {
		return fmt.Errorf("debes indicar un nombre de proveedor (ej: forgen provider add custom --base-url http://localhost:8080/v1)")
	}
	config, err := app.ConfigService.Load(ctx)
	if err != nil {
		return err
	}

	preset, isPreset := domain.FindProviderPreset(effectiveName)
	existing, isConfigured := config.FindProvider(effectiveName)

	var base domain.ProviderConfig
	switch {
	case isPreset:
		base = preset.ToProviderConfig()
	case isConfigured:
		base = existing
	default:
		// Nuevo proveedor custom sin preset.
		base = domain.ProviderConfig{
			Name: effectiveName,
			Type: domain.ProviderTypeOpenAICompatible,
			Models: []string{},
		}
		if baseURLOverride == "" {
			// Si no hay preset ni base-url, no podemos continuar.
			return fmt.Errorf("proveedor %q desconocido y sin --base-url. Disponibles: %s", effectiveName, strings.Join(domain.PresetNames(), ", "))
		}
	}

	if baseURLOverride != "" {
		base.BaseURL = strings.TrimSpace(baseURLOverride)
	}
	if base.BaseURL == "" {
		return fmt.Errorf("debes indicar --base-url para el proveedor custom (ej: --base-url http://localhost:8080/v1)")
	}
	if cmd.Flags().Changed("api-key-env") {
		v, _ := cmd.Flags().GetString("api-key-env")
		base.APIKeyEnv = strings.TrimSpace(v)
	}
	if noAuth {
		base.APIKeyEnv = ""
	}
	if cmd.Flags().Changed("models") {
		modelsFlag = strings.TrimSpace(modelsFlag)
		if modelsFlag == "" {
			base.Models = nil
		} else {
			base.Models = splitCSV(modelsFlag)
		}
	}

	isLocal := base.APIKeyEnv == "" || noAuth

	_, _ = fmt.Fprintf(out, "Proveedor: %s\nBase URL:  %s\n", base.Name, base.BaseURL)
	if isLocal {
		_, _ = fmt.Fprintln(out, "Modo local sin auth (llama.cpp/vLLM/LM Studio). GET /v1/models para auto-detección.")
	}

	var secret string
	if noAuth {
		secret = ""
	} else if base.APIKeyEnv == "" {
		// Local sin env: key opcional.
		s, err := promptSecret(in, out, fmt.Sprintf("API key de %s [Enter para sin auth]: ", base.Name))
		if err != nil {
			return err
		}
		secret = strings.TrimSpace(s)
	} else {
		s, err := promptSecret(in, out, fmt.Sprintf("API key de %s: ", base.Name))
		if err != nil {
			return err
		}
		secret = strings.TrimSpace(s)
		if secret == "" {
			return fmt.Errorf("API key vacía")
		}
	}

	// Auto-detección de modelos vía GET /v1/models si no se forzaron por flag.
	// ValidateProviderKey ya hace fallback a config.Models o ["default"] para locales.
	models, err := app.ValidateProviderKey(ctx, base, secret)
	if err != nil {
		return fmt.Errorf("no se pudo validar contra el proveedor: %w", err)
	}
	if cmd.Flags().Changed("models") && len(splitCSV(modelsFlag)) > 0 {
		// Si el usuario forzó --models, respetar ese listado.
		forced := splitCSV(modelsFlag)
		if len(forced) > 0 {
			models = forced
		}
	}
	_, _ = fmt.Fprintf(out, "✓ Proveedor validado. %d modelos disponibles.\n", len(models))

	if secret != "" {
		if err := app.Credentials.Set(ctx, apppkg.ProviderCredentialKey(base.Name), secret); err != nil {
			return fmt.Errorf("guardar credencial: %w", err)
		}
	} else {
		// Sin secret: borrar credencial previa si existía, para no dejar key vieja.
		_ = app.Credentials.Delete(ctx, apppkg.ProviderCredentialKey(base.Name))
	}

	base.Models = models
	defaultModel := ""
	if len(models) > 0 {
		defaultModel = models[0]
	}
	if err := app.AddProvider(ctx, base, defaultModel); err != nil {
		return fmt.Errorf("guardar configuración: %w", err)
	}

	_, _ = fmt.Fprintf(out, "\nModelos guardados para %s (%d):\n", base.Name, len(models))
	for _, model := range models {
		_, _ = fmt.Fprintf(out, "  - %s\n", model)
	}
	if secret != "" {
		_, _ = fmt.Fprintln(out, "\nListo. La API key se guardó en el almacén seguro del sistema.")
	} else {
		_, _ = fmt.Fprintln(out, "\nListo. Proveedor local sin API key (config.yaml actualizado).")
	}
	return nil
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
	isLocalNoAuth := base.APIKeyEnv == ""
	var secret string
	if isLocalNoAuth {
		s, err := promptSecret(in, out, fmt.Sprintf("API key de %s [Enter para sin auth]: ", base.Name))
		if err != nil {
			return err
		}
		secret = strings.TrimSpace(s)
	} else {
		s, err := promptSecret(in, out, fmt.Sprintf("API key de %s: ", base.Name))
		if err != nil {
			return err
		}
		secret = strings.TrimSpace(s)
		if secret == "" {
			return fmt.Errorf("API key vacía")
		}
	}

	// Validar contra el proveedor y detectar modelos disponibles del usuario.
	models, err := app.ValidateProviderKey(ctx, base, secret)
	if err != nil {
		return fmt.Errorf("no se pudo validar la key contra el proveedor: %w", err)
	}
	_, _ = fmt.Fprintf(out, "✓ Autenticación correcta. %d modelos disponibles.\n", len(models))

	// Guardar el secreto en el almacén seguro (nunca en config.yaml) solo si hay.
	if secret != "" {
		if err := app.Credentials.Set(ctx, apppkg.ProviderCredentialKey(base.Name), secret); err != nil {
			return fmt.Errorf("guardar credencial: %w", err)
		}
	} else {
		_ = app.Credentials.Delete(ctx, apppkg.ProviderCredentialKey(base.Name))
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
	if secret != "" {
		_, _ = fmt.Fprintln(out, "\nListo. La API key se guardó en el almacén seguro del sistema.")
	} else {
		_, _ = fmt.Fprintln(out, "\nListo. Proveedor local sin API key (config.yaml actualizado).")
	}
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
		if provider.APIKeyEnv == "" {
			cred = "local sin auth"
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

func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
