package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// pickerKind identifica qué acción dispara el picker al seleccionar.
type pickerKind int

const (
	pickerProviderKind pickerKind = iota
	pickerModelKind
	pickerSessionKind
	pickerTaskKind
)

// pickerItem es una opción del selector.
type pickerItem struct {
	label  string
	detail string
	value  string
}

// pickerModel es un selector de lista genérico (proveedores y modelos) que se
// abre en pantalla completa, igual que el asistente /init.
type pickerModel struct {
	kind   pickerKind
	title  string
	items  []pickerItem
	cursor int
	width  int
	height int
	styles Styles
	notice string
	err    string
}

// pickerSelectedMsg avisa al Model principal de una selección.
type pickerSelectedMsg struct {
	kind  pickerKind
	value string
}

// pickerCancelledMsg avisa al Model principal de una cancelación.
type pickerCancelledMsg struct{}

// pickerModelsMsg transporta el resultado de listar modelos en vivo.
type pickerModelsMsg struct {
	provider string
	models   []string
	err      error
}

func newPickerModel(kind pickerKind, title string, items []pickerItem, styles Styles, width, height int) *pickerModel {
	return &pickerModel{
		kind:   kind,
		title:  title,
		items:  items,
		styles: styles,
		width:  width,
		height: height,
	}
}

// setModels sustituye las opciones por una lista de modelos (listado en vivo).
func (p *pickerModel) setModels(models []string) {
	if len(models) == 0 {
		return
	}
	p.items = make([]pickerItem, 0, len(models))
	for _, model := range models {
		p.items = append(p.items, pickerItem{label: model, value: model})
	}
	if p.cursor >= len(p.items) {
		p.cursor = len(p.items) - 1
	}
	p.notice = fmt.Sprintf("%d modelos disponibles", len(models))
}

// Update implementa tea.Model. Devuelve el picker actualizado (puntero) y un
// comando opcional (selección o cancelación).
func (p *pickerModel) Update(message tea.Msg) (*pickerModel, tea.Cmd) {
	switch typedMessage := message.(type) {
	case tea.WindowSizeMsg:
		p.width = typedMessage.Width
		p.height = typedMessage.Height
	case tea.KeyMsg:
		switch typedMessage.String() {
		case "ctrl+c", "esc":
			return p, func() tea.Msg { return pickerCancelledMsg{} }
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if p.cursor < len(p.items)-1 {
				p.cursor++
			}
		case "enter":
			if len(p.items) == 0 {
				p.err = "Sin opciones disponibles. Usa /init primero."
				return p, nil
			}
			value := p.items[p.cursor].value
			return p, func() tea.Msg { return pickerSelectedMsg{kind: p.kind, value: value} }
		}
	}
	return p, nil
}

// View implementa tea.Model.
func (p *pickerModel) View() string {
	var builder strings.Builder
	builder.WriteString(p.styles.accent.Render(p.title))
	builder.WriteString("\n\n")

	if len(p.items) == 0 {
		builder.WriteString(p.styles.dim.Render("(sin opciones — usa /init para configurar un proveedor)"))
		builder.WriteString("\n\n" + p.styles.dim.Render("(Esc para volver)"))
		return builder.String()
	}

	window := 14
	if p.height > 0 {
		window = p.height - 8
	}
	if window < 5 {
		window = 5
	}
	start := 0
	if p.cursor >= window {
		start = p.cursor - window + 1
	}
	end := start + window
	if end > len(p.items) {
		end = len(p.items)
	}
	for i := start; i < end; i++ {
		item := p.items[i]
		marker := "  "
		if i == p.cursor {
			marker = "▸ "
		}
		if i == p.cursor {
			line := p.styles.accent.Render(marker) + item.label
			if item.detail != "" {
				line += "  " + p.styles.dim.Render(item.detail)
			}
			builder.WriteString(line + "\n")
			continue
		}
		line := marker + item.label
		if item.detail != "" {
			line += "  " + p.styles.dim.Render(item.detail)
		}
		builder.WriteString(line + "\n")
	}
	if p.notice != "" {
		builder.WriteString("\n" + p.styles.toolDone.Render(p.notice))
	}
	if p.err != "" {
		builder.WriteString("\n" + p.styles.err.Render(p.err))
	}
	builder.WriteString("\n\n" + p.styles.dim.Render("(↑/↓ mover · Enter elegir · Esc volver)"))
	return builder.String()
}
