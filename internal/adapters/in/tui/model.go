package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	apppkg "github.com/rodascaar/forgen/internal/app"
	"github.com/rodascaar/forgen/internal/application/agent"
	"github.com/rodascaar/forgen/internal/core/domain"
)

// spinnerFrames son los estados del indicador de actividad.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// transcriptLine es una línea renderizada de la conversación.
type transcriptLine struct {
	kind string
	text string
}

// Model es el estado de la TUI.
type Model struct {
	app             *apppkg.App
	program         *tea.Program
	input           textinput.Model
	styles          Styles
	transcript      []transcriptLine
	assistantBuffer string
	running         bool
	confirming      bool
	confirmCall     domain.ToolCall
	confirmCh       chan bool
	agentName       string
	modelKey        string
	phase           string
	sessionID       string
	workspace       string
	spinnerIndex    int
	width           int
	height          int
	quitting        bool
	cancelRun       context.CancelFunc
}

// Run inicia la TUI en modo pantalla alternativa.
func Run(app *apppkg.App) error {
	model := newModel(app)
	program := tea.NewProgram(model, tea.WithAltScreen())
	model.program = program
	_, err := program.Run()
	return err
}

func newModel(app *apppkg.App) Model {
	input := textinput.New()
	input.Placeholder = "Describe tu tarea... (Enter envía, Tab cambia agente, q sale)"
	input.Prompt = "❯ "
	input.Width = 80

	workspace, _ := os.Getwd()
	return Model{
		app:        app,
		input:      input,
		styles:     newStyles(domain.DefaultTheme()),
		agentName:  "build",
		workspace:  workspace,
		transcript: make([]transcriptLine, 0, 64),
	}
}

// Init implementa tea.Model.
func (m Model) Init() tea.Cmd {
	appConfig, err := m.app.LoadConfig(context.Background())
	if err != nil {
		m.append("error", fmt.Sprintf("config: %v", err))
		return nil
	}
	m.agentName = appConfig.Agent
	if m.agentName == "" {
		m.agentName = "build"
	}
	m.styles = newStyles(appConfig.Theme)
	m.modelKey = fmt.Sprintf("%s/%s", appConfig.Default.Provider, appConfig.Default.Model)
	if appConfig.Default.Provider == "" || appConfig.Default.Model == "" {
		m.append("error", "No hay modelo configurado. Ejecuta 'forgen auth' o 'forgen init'.")
	} else {
		m.append("notice", fmt.Sprintf("forgen — agente %s · modelo %s · cwd %s", m.agentName, m.modelKey, m.workspace))
	}
	m.input.Focus()
	return textinput.Blink
}

// Update implementa tea.Model.
func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch typedMessage := message.(type) {
	case tea.WindowSizeMsg:
		m.width = typedMessage.Width
		m.height = typedMessage.Height
		m.input.Width = m.width - 4
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(typedMessage)

	case streamDeltaMsg:
		m.assistantBuffer += typedMessage.text
		return m, nil

	case toolStartedMsg:
		m.flushAssistant()
		m.append("tool", fmt.Sprintf("▶ %s", toolCallLabel(typedMessage.call)))
		return m, nil

	case toolFinishedMsg:
		if typedMessage.result.OK {
			m.append("tool_done", fmt.Sprintf("✓ %s %s", typedMessage.call.Name, summarize(typedMessage.result.Output)))
		} else {
			m.append("error", fmt.Sprintf("✗ %s: %v", typedMessage.call.Name, typedMessage.result.Error))
		}
		return m, nil

	case noticeMsg:
		m.flushAssistant()
		m.append("notice", typedMessage.text)
		return m, nil

	case errorMsg:
		m.flushAssistant()
		m.append("error", fmt.Sprintf("Error: %v", typedMessage.err))
		m.running = false
		return m, nil

	case finishedMsg:
		m.flushAssistant()
		m.running = false
		return m, nil

	case runDoneMsg:
		if typedMessage.err != nil {
			m.append("error", fmt.Sprintf("Error: %v", typedMessage.err))
		}
		if typedMessage.sessionID != "" {
			m.sessionID = typedMessage.sessionID
		}
		if typedMessage.modelKey != "" {
			m.modelKey = typedMessage.modelKey
		}
		if typedMessage.phase != "" {
			m.phase = typedMessage.phase
		}
		m.running = false
		return m, nil

	case confirmRequestMsg:
		m.confirming = true
		m.confirmCall = typedMessage.call
		m.confirmCh = typedMessage.response
		return m, nil

	case tickMsg:
		if m.running {
			m.spinnerIndex = (m.spinnerIndex + 1) % len(spinnerFrames)
			return m, tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{} })
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Mientras se confirma un permiso, las teclas son respuestas Y/N.
	if m.confirming {
		switch message.String() {
		case "y", "Y":
			m.confirming = false
			m.confirmCh <- true
			m.append("notice", fmt.Sprintf("Permiso concedido: %s", toolCallLabel(m.confirmCall)))
		case "n", "N", "esc", "ctrl+c":
			m.confirming = false
			m.confirmCh <- false
			m.append("notice", fmt.Sprintf("Permiso denegado: %s", toolCallLabel(m.confirmCall)))
		}
		return m, nil
	}

	switch message.String() {
	case "ctrl+c":
		if m.running {
			if m.cancelRun != nil {
				m.cancelRun()
			}
			m.append("notice", "Cancelando petición...")
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit

	case "q":
		// 'q' sale solo con el prompt vacío; si hay texto, se deja escribir
		// la letra 'q' en el campo de entrada.
		if m.input.Value() != "" {
			break
		}
		if m.running {
			if m.cancelRun != nil {
				m.cancelRun()
			}
			m.append("notice", "Cancelando petición...")
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit

	case "tab":
		if m.running {
			return m, nil
		}
		m.toggleAgent()
		return m, nil

	case "enter":
		prompt := strings.TrimSpace(m.input.Value())
		if prompt == "" || m.running {
			return m, nil
		}
		m.input.SetValue("")
		m.append("user", prompt)
		// Activar el estado de ejecución AQUÍ: startRun recibe m por valor,
		// así que cualquier mutación interna se descarta. Sin esto el spinner,
		// el bloqueo de Enter, el cancelado con Ctrl+C y el bloqueo de Tab
		// nunca funcionan.
		m.running = true
		m.assistantBuffer = ""
		ctx, cancel := context.WithCancel(context.Background())
		m.cancelRun = cancel
		return m, m.startRun(ctx, prompt)
	}

	var command tea.Cmd
	m.input, command = m.input.Update(message)
	return m, command
}

// toggleAgent alterna entre build y plan.
func (m *Model) toggleAgent() {
	agents := []string{"build", "plan"}
	for index, name := range agents {
		if name == m.agentName {
			m.agentName = agents[(index+1)%len(agents)]
			m.append("notice", fmt.Sprintf("Agente: %s", m.agentName))
			return
		}
	}
	m.agentName = "build"
}

// startRun arma el comando del agente en segundo plano + el ticker del spinner.
// El estado m.running ya se activó en handleKey (este método recibe m por valor).
func (m Model) startRun(ctx context.Context, prompt string) tea.Cmd {
	runCommand := func() tea.Msg {
		return runAgent(ctx, m.app, m.sessionID, m.workspace, m.agentName, prompt, m.program)
	}
	ticker := tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{} })
	return tea.Batch(runCommand, ticker)
}

// runAgent ejecuta un turno del agente en segundo plano.
func runAgent(ctx context.Context, app *apppkg.App, sessionID, workspace, agentName, prompt string,
	program *tea.Program) tea.Msg {
	appConfig, err := app.LoadConfig(ctx)
	if err != nil {
		return runDoneMsg{err: err}
	}

	model, provider, phase, err := app.ResolveRunModel(ctx, prompt, "", "")
	if err != nil {
		return runDoneMsg{err: err}
	}
	agentDef, err := app.SelectedAgent(appConfig, agentName)
	if err != nil {
		return runDoneMsg{err: err}
	}

	// Primera ejecución: crear la sesión.
	session := loadOrCreateSessionTUI(ctx, app, sessionID, workspace, model, agentDef.Name)
	messenger := newTUIMessenger(program)

	runner, err := app.NewRunner(ctx, apppkg.RunnerDeps{
		Provider:  provider,
		Model:     model,
		Agent:     agentDef,
		Messenger: messenger,
		Responder: messenger,
		Workspace: workspace,
		SessionID: session.ID,
	})
	if err != nil {
		return runDoneMsg{err: err}
	}
	result, err := runner.Run(ctx, agent.RunInput{
		Session:    session,
		Agent:      agentDef,
		Workspace:  workspace,
		UserPrompt: prompt,
		Phase:      phase,
	})
	if err != nil {
		return runDoneMsg{err: err}
	}
	_ = app.SessionService.Save(ctx, result.Session)
	return runDoneMsg{err: nil, sessionID: result.Session.ID, modelKey: model.Key(), phase: string(phase)}
}

func loadOrCreateSessionTUI(ctx context.Context, app *apppkg.App, sessionID, workspace string,
	model domain.Model, agentName string) domain.Session {
	if sessionID != "" {
		if session, err := app.SessionService.Resume(ctx, sessionID); err == nil {
			return session
		}
	}
	session, err := app.SessionService.Create(ctx, workspace, model, agentName)
	if err != nil {
		return domain.Session{ID: sessionID, Workspace: workspace, Model: model, Agent: agentName}
	}
	return session
}

// View implementa tea.Model.
func (m Model) View() string {
	if m.quitting {
		return "Hasta la próxima.\n"
	}

	availableHeight := m.height - 4
	if availableHeight < 5 {
		availableHeight = 5
	}
	body := m.renderTranscript(availableHeight)
	status := m.renderStatus()
	inputLine := m.renderInput()

	return strings.Join([]string{body, status, inputLine}, "\n")
}

func (m Model) renderTranscript(limit int) string {
	lines := m.transcript
	// Añadir el buffer "vivo" del asistente mientras genera, para que el
	// texto del streaming se vea en tiempo real y la TUI no parezca colgada.
	if m.assistantBuffer != "" {
		lines = append(append([]transcriptLine{}, lines...), transcriptLine{kind: "assistant", text: m.assistantBuffer})
	}
	if len(lines) == 0 {
		return m.styles.dim.Render(" (sin conversación) ")
	}
	start := len(lines) - limit
	if start < 0 {
		start = 0
	}
	lines = lines[start:]
	rendered := make([]string, 0, len(lines))
	for _, line := range lines {
		rendered = append(rendered, m.styles.forKind(line.kind).Render(line.text))
	}
	return strings.Join(rendered, "\n")
}

func (m Model) renderStatus() string {
	left := ""
	if m.running {
		left += m.styles.accent.Render(spinnerFrames[m.spinnerIndex])
	} else {
		left += m.styles.dim.Render("●")
	}
	left += " " + m.styles.dim.Render(fmt.Sprintf("agente:%s", m.agentName))
	left += " " + m.styles.dim.Render(fmt.Sprintf("modelo:%s", m.modelKey))
	if m.phase != "" {
		left += " " + m.styles.accent.Render(fmt.Sprintf("fase:%s", m.phase))
	}
	right := ""
	if m.confirming {
		right = m.styles.notice.Render(fmt.Sprintf("¿Permitir %s? (y/n)", toolCallLabel(m.confirmCall)))
	} else if m.running {
		right = m.styles.dim.Render("trabajando...")
	}
	if m.width > 0 {
		gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
		if gap < 1 {
			gap = 1
		}
		return left + strings.Repeat(" ", gap) + right
	}
	return left + "  " + right
}

func (m Model) renderInput() string {
	if m.confirming {
		return m.styles.notice.Render(fmt.Sprintf("❯ ¿Permitir ejecutar %s? [y/N]", toolCallLabel(m.confirmCall)))
	}
	return m.input.View()
}

// flushAssistant vuelca el texto acumulado del asistente al transcript.
func (m *Model) flushAssistant() {
	if m.assistantBuffer == "" {
		return
	}
	m.transcript = append(m.transcript, transcriptLine{kind: "assistant", text: m.assistantBuffer})
	m.assistantBuffer = ""
}

func (m *Model) append(kind, text string) {
	m.transcript = append(m.transcript, transcriptLine{kind: kind, text: text})
}

func summarize(text string) string {
	text = strings.TrimSpace(text)
	if len(text) > 80 {
		return text[:80] + "..."
	}
	return text
}

type tickMsg struct{}
