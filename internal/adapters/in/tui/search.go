package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	apppkg "github.com/rodascaar/forgen/internal/app"
)

// searchStage enumera las etapas del asistente de búsqueda web (/search).
type searchStage int

const (
	searchPickProvider searchStage = iota
	searchEnterKey
	searchDone
)

// searchModel configura la búsqueda web (Brave) desde la TUI.
// Guía al usuario: elegir proveedor (brave/off) → pegar API key → guardar.
// A diferencia del /init, no valida contra un endpoint (Brave no lista
// modelos), por lo que simplemente guarda provider + key.
type searchModel struct {
	app       *apppkg.App
	styles    Styles
	width     int
	height    int
	stage     searchStage
	providers []string
	cursor    int
	key       textinput.Model
	provider  string
	err       string
	notice    string
}

// searchDoneMsg avisa al Model principal que terminó el asistente de búsqueda.
type searchDoneMsg struct{}

// searchCancelMsg avisa al Model principal que el usuario canceló.
type searchCancelMsg struct{}

func newSearchModel(app *apppkg.App, styles Styles, width int) *searchModel {
	key := textinput.New()
	key.Placeholder = "Pega tu API key de Brave aquí..."
	key.EchoMode = textinput.EchoPassword
	key.Width = 60
	return &searchModel{
		app:       app,
		styles:    styles,
		width:     width,
		stage:     searchPickProvider,
		providers: []string{"brave", "off"},
		key:       key,
	}
}

// Init implementa tea.Model.
func (w *searchModel) Init() tea.Cmd { return nil }

// Update implementa tea.Model.
func (w *searchModel) Update(message tea.Msg) (*searchModel, tea.Cmd) {
	switch typedMessage := message.(type) {
	case tea.WindowSizeMsg:
		w.width = typedMessage.Width
		w.height = typedMessage.Height
		return w, nil
	case tea.KeyMsg:
		return w.handleKey(typedMessage)
	}
	return w, nil
}

func (w *searchModel) handleKey(message tea.KeyMsg) (*searchModel, tea.Cmd) {
	switch w.stage {
	case searchPickProvider:
		switch message.String() {
		case "ctrl+c", "esc":
			return w, w.cancel()
		case "up", "k":
			if w.cursor > 0 {
				w.cursor--
			}
		case "down", "j":
			if w.cursor < len(w.providers)-1 {
				w.cursor++
			}
		case "enter":
			w.provider = w.providers[w.cursor]
			w.err = ""
			if w.provider == "off" {
				return w, w.saveOff()
			}
			w.key.SetValue("")
			w.key.Focus()
			w.stage = searchEnterKey
		}

	case searchEnterKey:
		switch message.String() {
		case "ctrl+c", "esc":
			w.key.Blur()
			w.stage = searchPickProvider
		case "enter":
			apiKey := strings.TrimSpace(w.key.Value())
			if apiKey == "" {
				w.err = "La API key no puede estar vacía."
				return w, nil
			}
			w.err = ""
			return w, w.saveBrave(apiKey)
		default:
			var cmd tea.Cmd
			w.key, cmd = w.key.Update(message)
			return w, cmd
		}

	case searchDone:
		switch message.String() {
		case "enter", "esc", "ctrl+c":
			return w, w.finish()
		}
	}
	return w, nil
}

// saveOff deshabilita la búsqueda web y guarda la config.
func (w *searchModel) saveOff() tea.Cmd {
	if w.app == nil {
		w.err = "app no inicializada"
		w.stage = searchPickProvider
		return nil
	}
	ctx := context.Background()
	config, err := w.app.ConfigService.Load(ctx)
	if err != nil {
		w.err = err.Error()
		w.stage = searchPickProvider
		return nil
	}
	config.Search.Provider = ""
	if err := w.app.ConfigService.Save(ctx, config); err != nil {
		w.err = err.Error()
		w.stage = searchPickProvider
		return nil
	}
	w.notice = "✓ Búsqueda web deshabilitada."
	w.stage = searchDone
	return nil
}

// saveBrave guarda la API key de Brave en el credential store y activa el
// provider en la config.
func (w *searchModel) saveBrave(apiKey string) tea.Cmd {
	ctx := context.Background()
	if w.app == nil {
		w.err = "app no inicializada"
		w.stage = searchEnterKey
		return nil
	}
	if w.app.Credentials == nil {
		w.err = "No hay almacén de credenciales disponible."
		w.stage = searchEnterKey
		return nil
	}
	config, err := w.app.ConfigService.Load(ctx)
	if err != nil {
		w.err = err.Error()
		w.stage = searchEnterKey
		return nil
	}
	config.Search.Provider = "brave"
	config.Search.APIKeyEnv = "BRAVE_API_KEY"
	if err := w.app.ConfigService.Save(ctx, config); err != nil {
		w.err = err.Error()
		w.stage = searchEnterKey
		return nil
	}
	if err := w.app.Credentials.Set(ctx, apppkg.SearchCredentialKey("brave"), apiKey); err != nil {
		w.err = fmt.Sprintf("no se pudo guardar la API key: %v", err)
		w.stage = searchEnterKey
		return nil
	}
	w.notice = "✓ Búsqueda web (Brave) activada."
	w.stage = searchDone
	return nil
}

func (w *searchModel) finish() tea.Cmd {
	return func() tea.Msg { return searchDoneMsg{} }
}

func (w *searchModel) cancel() tea.Cmd {
	return func() tea.Msg { return searchCancelMsg{} }
}

// View implementa tea.Model.
func (w *searchModel) View() string {
	var builder strings.Builder
	builder.WriteString(w.styles.accent.Render("Búsqueda web — configurar"))
	builder.WriteString("\n\n")

	switch w.stage {
	case searchPickProvider:
		builder.WriteString("Elige el proveedor de búsqueda web (↑/↓, Enter para seleccionar):\n\n")
		for i, name := range w.providers {
			marker := "  "
			if i == w.cursor {
				marker = "▸ "
			}
			desc := "Búsqueda web con Brave Search (requiere API key)"
			if name == "off" {
				desc = "Deshabilitar la búsqueda web"
			}
			line := fmt.Sprintf("%s%-8s %s", marker, name, desc)
			if i == w.cursor {
				line = w.styles.accent.Render(line)
			}
			builder.WriteString(line + "\n")
		}
		builder.WriteString("\n" + w.styles.dim.Render("(Esc cancela)"))

	case searchEnterKey:
		builder.WriteString("Proveedor: brave\n\n")
		builder.WriteString("Pega tu API key de Brave (no se mostrará en pantalla):\n")
		builder.WriteString("(la obtienes en https://api.search.brave.com)\n\n")
		builder.WriteString(w.key.View() + "\n")
		if w.err != "" {
			builder.WriteString("\n" + w.styles.err.Render(w.err) + "\n")
		}
		builder.WriteString("\n" + w.styles.dim.Render("(Enter guarda · Esc vuelve)"))

	case searchDone:
		builder.WriteString(w.styles.toolDone.Render(w.notice) + "\n\n")
		builder.WriteString(w.styles.dim.Render("(Enter para continuar)"))
	}
	return builder.String()
}
