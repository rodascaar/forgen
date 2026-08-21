package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	apppkg "github.com/rodascaar/forgen/internal/app"
	"github.com/rodascaar/forgen/internal/core/domain"
)

// wizardStage enumera las etapas del asistente de configuración.
type wizardStage int

const (
	wizPickProvider wizardStage = iota
	wizEnterKey
	wizValidating
	wizDone
)

// wizardModel es el asistente de configuración rápida que se lanza con /init.
// Guía al usuario: elegir proveedor → pegar API key → validar → guardar.
type wizardModel struct {
	app      *apppkg.App
	styles   Styles
	width    int
	height   int
	stage    wizardStage
	presets  []domain.ProviderPreset
	cursor   int
	key      textinput.Model
	provider domain.ProviderPreset
	err      string
	notice   string
}

// wizardDoneMsg avisa al Model principal que el setup terminó correctamente.
type wizardDoneMsg struct{}

// wizardCancelMsg avisa al Model principal que el usuario canceló el asistente.
type wizardCancelMsg struct{}

// wizardKeyValidatedMsg transporta el resultado de validar la API key.
type wizardKeyValidatedMsg struct {
	provider domain.ProviderConfig
	models   []string
	err      error
}

func newWizardModel(app *apppkg.App, styles Styles, width int) *wizardModel {
	key := textinput.New()
	key.Placeholder = "Pega tu API key aquí..."
	key.EchoMode = textinput.EchoPassword
	key.Width = 60
	return &wizardModel{
		app:     app,
		styles:  styles,
		width:   width,
		stage:   wizPickProvider,
		presets: domain.ProviderPresets(),
		key:     key,
	}
}

// Init implementa tea.Model.
func (w *wizardModel) Init() tea.Cmd {
	return nil
}

// Update implementa tea.Model. Devuelve el wizard actualizado (puntero) y un
// comando opcional (validación async o fin de asistente).
func (w *wizardModel) Update(message tea.Msg) (*wizardModel, tea.Cmd) {
	switch typedMessage := message.(type) {
	case tea.WindowSizeMsg:
		w.width = typedMessage.Width
		w.height = typedMessage.Height
		return w, nil

	case wizardKeyValidatedMsg:
		if typedMessage.err != nil {
			w.stage = wizEnterKey
			w.err = fmt.Sprintf("No se pudo validar la key: %v", typedMessage.err)
			return w, nil
		}
		if err := w.save(typedMessage.provider, typedMessage.models); err != nil {
			w.stage = wizEnterKey
			w.err = err.Error()
			return w, nil
		}
		w.stage = wizDone
		w.notice = fmt.Sprintf("✓ %s configurado. %d modelos disponibles.",
			typedMessage.provider.Name, len(typedMessage.models))
		return w, nil

	case tea.KeyMsg:
		return w.handleKey(typedMessage)
	}
	return w, nil
}

func (w *wizardModel) handleKey(message tea.KeyMsg) (*wizardModel, tea.Cmd) {
	switch w.stage {
	case wizPickProvider:
		switch message.String() {
		case "ctrl+c", "esc":
			return w, w.cancel()
		case "up", "k":
			if w.cursor > 0 {
				w.cursor--
			}
		case "down", "j":
			if w.cursor < len(w.presets)-1 {
				w.cursor++
			}
		case "enter":
			w.provider = w.presets[w.cursor]
			w.key.SetValue("")
			w.err = ""
			w.key.Focus()
			w.stage = wizEnterKey
		}

	case wizEnterKey:
		switch message.String() {
		case "ctrl+c", "esc":
			w.key.Blur()
			w.stage = wizPickProvider
		case "enter":
			apiKey := strings.TrimSpace(w.key.Value())
			if apiKey == "" {
				w.err = "La API key no puede estar vacía."
				return w, nil
			}
			w.err = ""
			w.stage = wizValidating
			cfg := w.provider.ToProviderConfig()
			return w, w.validate(context.Background(), cfg, apiKey)
		default:
			var cmd tea.Cmd
			w.key, cmd = w.key.Update(message)
			return w, cmd
		}

	case wizDone:
		switch message.String() {
		case "enter", "esc", "ctrl+c":
			return w, w.finish()
		}
	}
	return w, nil
}

// validate consulta los modelos del proveedor con la key dada (async).
func (w *wizardModel) validate(ctx context.Context, cfg domain.ProviderConfig, apiKey string) tea.Cmd {
	return func() tea.Msg {
		models, err := w.app.ValidateProviderKey(ctx, cfg, apiKey)
		return wizardKeyValidatedMsg{provider: cfg, models: models, err: err}
	}
}

// save guarda la credencial y la metadata del proveedor.
func (w *wizardModel) save(cfg domain.ProviderConfig, models []string) error {
	if err := w.app.Credentials.Set(context.Background(),
		apppkg.ProviderCredentialKey(cfg.Name), w.key.Value()); err != nil {
		return fmt.Errorf("no se pudo guardar la API key: %w", err)
	}
	cfg.Models = models
	defaultModel := ""
	if len(models) > 0 {
		defaultModel = models[0]
	}
	if err := w.app.AddProvider(context.Background(), cfg, defaultModel); err != nil {
		return fmt.Errorf("no se pudo guardar la configuración: %w", err)
	}
	return nil
}

func (w *wizardModel) finish() tea.Cmd {
	return func() tea.Msg { return wizardDoneMsg{} }
}

func (w *wizardModel) cancel() tea.Cmd {
	return func() tea.Msg { return wizardCancelMsg{} }
}

// View implementa tea.Model.
func (w *wizardModel) View() string {
	var builder strings.Builder
	builder.WriteString(w.styles.accent.Render("Configuración rápida de forgen"))
	builder.WriteString("\n\n")

	switch w.stage {
	case wizPickProvider:
		builder.WriteString("Elige tu proveedor (↑/↓, Enter para seleccionar):\n\n")
		window := 12
		if w.height > 0 {
			window = w.height - 8
		}
		if window < 5 {
			window = 5
		}
		start := 0
		if w.cursor >= window {
			start = w.cursor - window + 1
		}
		end := start + window
		if end > len(w.presets) {
			end = len(w.presets)
		}
		for i := start; i < end; i++ {
			preset := w.presets[i]
			marker := "  "
			if i == w.cursor {
				marker = "▸ "
			}
			line := fmt.Sprintf("%s%-12s %s", marker, preset.Name, preset.BaseURL)
			if i == w.cursor {
				line = w.styles.accent.Render(line)
			}
			builder.WriteString(line + "\n")
		}
		builder.WriteString("\n" + w.styles.dim.Render("(Esc cancela)"))

	case wizEnterKey:
		fmt.Fprintf(&builder, "Proveedor: %s\nBase URL:  %s\n\n",
			w.provider.Name, w.provider.BaseURL)
		builder.WriteString("Pega tu API key (no se mostrará en pantalla):\n\n")
		builder.WriteString(w.key.View() + "\n")
		if w.err != "" {
			builder.WriteString("\n" + w.styles.err.Render(w.err) + "\n")
		}
		builder.WriteString("\n" + w.styles.dim.Render("(Enter valida · Esc vuelve)"))

	case wizValidating:
		fmt.Fprintf(&builder, "Validando la API key contra %s...", w.provider.Name)

	case wizDone:
		builder.WriteString(w.styles.toolDone.Render(w.notice) + "\n\n")
		builder.WriteString(w.styles.dim.Render("(Enter para empezar a usar forgen)"))
	}
	return builder.String()
}
