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

// modelListTimeout acota la validación/listado de modelos del wizard y /model
// para que la UI nunca parezca congelada si el proveedor tarda.
const modelListTimeout = 15 * time.Second

// transcriptLine es una línea renderizada de la conversación.
type transcriptLine struct {
	kind string
	text string
}

// slashHelpText resume los comandos disponibles dentro de la TUI.
const slashHelpText = `Comandos: /init configura tu proveedor y API key · /help esta ayuda · /quit sale
Atajos: Enter envía · Tab cambia agente · ? ayuda · PgUp/PgDn desplazan la conversación · Ctrl+C cancela`

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
	noConfig        bool
	scrollOffset    int
	wizard          *wizardModel
	picker          *pickerModel
	helpOpen        bool
}

// Run inicia la TUI en modo pantalla alternativa.
func Run(app *apppkg.App) error {
	model := newModel(app)
	program := tea.NewProgram(model, tea.WithAltScreen())
	model.program = program
	_, err := program.Run()
	return err
}

// newModel construye el estado inicial cargando la configuración. Se construye
// y devuelve el valor completo, por lo que los mensajes de onboarding persisten
// (a diferencia de Init(), que solo devuelve el Cmd y descarta mutaciones).
func newModel(app *apppkg.App) Model {
	input := textinput.New()
	input.Placeholder = "Describe tu tarea... (/ para comandos, ? ayuda, Enter envía)"
	input.Prompt = "❯ "
	input.Width = 80
	// El foco debe quedar en el input del modelo vivo. No se puede hacer en
	// Init() porque tiene receiver por valor y descarta las mutaciones; aquí
	// (al construir el modelo que se devuelve) el foco sí persiste.
	input.Focus()

	workspace, _ := os.Getwd()
	m := Model{
		app:        app,
		input:      input,
		styles:     newStyles(domain.DefaultTheme()),
		agentName:  "build",
		workspace:  workspace,
		transcript: make([]transcriptLine, 0, 64),
	}
	m.loadConfigInto()
	return m
}

// loadConfigInto carga la configuración, detecta si hay un proveedor usable y
// prepara la barra de estado y el mensaje de onboarding. Los mensajes se anexan
// al transcript; como se invoca construyendo el modelo (no en Init), persisten.
func (m *Model) loadConfigInto() {
	appConfig, err := m.app.LoadConfig(context.Background())
	if err != nil {
		m.append("error", fmt.Sprintf("config: %v", err))
		return
	}
	m.applyConfig(appConfig)
	if m.noConfig {
		m.append("error", "No hay modelo configurado ni API key disponible.")
		m.append("notice", "Escribe /init para configurar tu proveedor y API key, o corre 'forgen init'.")
		m.append("notice", slashHelpText)
		return
	}
	m.append("notice", fmt.Sprintf("forgen — agente %s · modelo %s · cwd %s",
		m.agentName, m.modelKey, m.workspace))
}

// applyConfig actualiza agente, tema, modelo por defecto y el flag noConfig
// sin anexar mensajes (las acciones de picker anexan su propia notificación).
func (m *Model) applyConfig(appConfig domain.AppConfig) {
	m.agentName = appConfig.Agent
	if m.agentName == "" {
		m.agentName = "build"
	}
	m.styles = newStyles(appConfig.Theme)
	configured := false
	if appConfig.Default.Provider != "" && appConfig.Default.Model != "" {
		if provider, ok := appConfig.FindProvider(appConfig.Default.Provider); ok {
			configured = m.app.ProviderUsable(provider)
		}
	}
	m.modelKey = fmt.Sprintf("%s/%s", appConfig.Default.Provider, appConfig.Default.Model)
	m.noConfig = !configured
}

// refreshFromConfig recarga la config tras un /init completado.
func (m *Model) refreshFromConfig() {
	appConfig, err := m.app.LoadConfig(context.Background())
	if err != nil {
		m.append("error", fmt.Sprintf("config: %v", err))
		return
	}
	wasUnconfigured := m.noConfig
	m.applyConfig(appConfig)
	if wasUnconfigured && !m.noConfig {
		m.append("notice", fmt.Sprintf("✓ Configuración lista. Modelo por defecto: %s", m.modelKey))
	}
}

// Init implementa tea.Model. El setup (incluido el foco del input) se hace en
// newModel para que persista; Init solo arranca el parpadeo del cursor.
func (m Model) Init() tea.Cmd {
	return textinput.Blink
}

// Update implementa tea.Model.
func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	// Mensajes que cierran/afectan a un sub-modelo (wizard/picker) se procesan
	// ANTES de delegar: si se delegaran, el sub-modelo los tragaría y el
	// asistente/selector nunca se cerraría (la UI se congelaba).
	switch typedMessage := message.(type) {
	case wizardDoneMsg:
		m.wizard = nil
		m.refreshFromConfig()
		return m, nil

	case wizardCancelMsg:
		m.wizard = nil
		return m, nil

	case pickerSelectedMsg:
		m.picker = nil
		m.applyPickerSelection(typedMessage)
		return m, nil

	case pickerCancelledMsg:
		m.picker = nil
		return m, nil

	case pickerModelsMsg:
		if typedMessage.err != nil {
			if m.picker != nil {
				m.picker.err = "No se pudieron cargar los modelos en vivo; mostrando los de la config."
			}
			return m, nil
		}
		if m.picker != nil {
			m.picker.setModels(typedMessage.models)
		}
		return m, nil
	}

	// Mientras un sub-modelo está activo, delegamos el resto de mensajes
	// (teclado, resize, resultado de validación).
	if m.wizard != nil {
		var cmd tea.Cmd
		m.wizard, cmd = m.wizard.Update(message)
		return m, cmd
	}
	if m.picker != nil {
		var cmd tea.Cmd
		m.picker, cmd = m.picker.Update(message)
		return m, cmd
	}

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
		m.resetConfirm()
		m.running = false
		return m, nil

	case finishedMsg:
		m.flushAssistant()
		m.resetConfirm()
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
		m.resetConfirm()
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

// resetConfirm limpia el estado del prompt de permiso. Se invoca al terminar o
// fallar una petición para que la UI nunca quede atascada en el modal Y/N.
func (m *Model) resetConfirm() {
	m.confirming = false
	m.confirmCall = domain.ToolCall{}
	m.confirmCh = nil
}

func (m Model) handleKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Overlay de ayuda: consume teclas hasta cerrarlo.
	if m.helpOpen {
		switch message.String() {
		case "esc", "q", "ctrl+c", "?":
			m.helpOpen = false
		}
		return m, nil
	}

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
		case "?", "h":
			m.append("notice", permissionDetail(m.confirmCall))
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

	case "?":
		m.helpOpen = true
		return m, nil

	case "pgup":
		m.scrollOffset += m.height - 4
		return m, nil

	case "pgdown":
		m.scrollOffset -= m.height - 4
		if m.scrollOffset < 0 {
			m.scrollOffset = 0
		}
		return m, nil

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
		// Comandos tipo slash (/init, /help, /quit).
		if strings.HasPrefix(prompt, "/") {
			return m.handleSlash(prompt)
		}
		if m.noConfig {
			m.append("notice", "Configura un proveedor primero: escribe /init")
			return m, nil
		}
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

// handleSlash procesa un comando slash escrito en el campo de entrada.
func (m Model) handleSlash(command string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return m, nil
	}
	switch fields[0] {
	case "/init":
		m.wizard = newWizardModel(m.app, m.styles, m.width)
		return m, m.wizard.Init()
	case "/provider":
		return m.openProviderPicker()
	case "/model":
		return m.openModelPicker()
	case "/sessions":
		return m.openSessionsPicker()
	case "/help", "/?":
		m.helpOpen = true
		return m, nil
	case "/quit", "/exit":
		m.quitting = true
		return m, tea.Quit
	default:
		m.append("error", fmt.Sprintf("Comando desconocido: %s (prueba /help)", fields[0]))
	}
	return m, nil
}

// openProviderPicker abre el selector de proveedores por defecto.
func (m Model) openProviderPicker() (tea.Model, tea.Cmd) {
	appConfig, err := m.app.LoadConfig(context.Background())
	if err != nil {
		m.append("error", fmt.Sprintf("Error: %v", err))
		return m, nil
	}
	if len(appConfig.Providers) == 0 {
		m.append("notice", "No hay proveedores configurados. Usa /init.")
		return m, nil
	}
	items := make([]pickerItem, 0, len(appConfig.Providers))
	for _, provider := range appConfig.Providers {
		label := provider.Name
		if provider.Name == appConfig.Default.Provider {
			label += " *"
		}
		detail := ""
		if len(provider.Models) > 0 {
			detail = provider.Models[0]
		}
		items = append(items, pickerItem{label: label, detail: detail, value: provider.Name})
	}
	m.picker = newPickerModel(pickerProviderKind, "Elige el proveedor por defecto", items, m.styles, m.width, m.height)
	return m, nil
}

// openModelPicker abre el selector de modelos del proveedor activo. Muestra los
// modelos de la config de inmediato y lanza un listado en vivo como respaldo.
func (m Model) openModelPicker() (tea.Model, tea.Cmd) {
	appConfig, err := m.app.LoadConfig(context.Background())
	if err != nil {
		m.append("error", fmt.Sprintf("Error: %v", err))
		return m, nil
	}
	if appConfig.Default.Provider == "" {
		m.append("notice", "Configura un proveedor primero: usa /init.")
		return m, nil
	}
	providerConfig, ok := appConfig.FindProvider(appConfig.Default.Provider)
	if !ok {
		m.append("error", fmt.Sprintf("Proveedor %q no configurado.", appConfig.Default.Provider))
		return m, nil
	}
	m.picker = m.modelPickerFor(providerConfig)
	// Listado en vivo de los modelos de la cuenta (si hay key guardada).
	cfg := providerConfig
	fetch := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), modelListTimeout)
		defer cancel()
		return pickerModelsMsg{provider: cfg.Name, models: m.app.ListModelsFor(ctx, cfg)}
	}
	return m, fetch
}

// openSessionsPicker abre el selector de sesiones guardadas para retomarlas.
func (m Model) openSessionsPicker() (tea.Model, tea.Cmd) {
	sessions, err := m.app.SessionService.List(context.Background(), 20)
	if err != nil {
		m.append("error", fmt.Sprintf("Error: %v", err))
		return m, nil
	}
	if len(sessions) == 0 {
		m.append("notice", "No hay sesiones guardadas todavía.")
		return m, nil
	}
	items := make([]pickerItem, 0, len(sessions))
	for _, session := range sessions {
		detail := fmt.Sprintf("%s · %s", session.Agent, session.Model.Key())
		if summary := session.Summary(); summary != "" {
			detail += " · " + summary
		}
		items = append(items, pickerItem{label: session.ID, detail: detail, value: session.ID})
	}
	m.picker = newPickerModel(pickerSessionKind, "Retomar una sesión", items, m.styles, m.width, m.height)
	return m, nil
}

// modelPickerFor construye el selector de modelos de un proveedor dado.
func (m Model) modelPickerFor(providerConfig domain.ProviderConfig) *pickerModel {	items := make([]pickerItem, 0, len(providerConfig.Models))
	for _, model := range providerConfig.Models {
		items = append(items, pickerItem{label: model, value: model})
	}
	return newPickerModel(pickerModelKind, "Modelo por defecto de "+providerConfig.Name, items, m.styles, m.width, m.height)
}

// applyPickerSelection aplica el resultado de un picker al config por defecto.
func (m *Model) applyPickerSelection(selection pickerSelectedMsg) {
	ctx := context.Background()
	switch selection.kind {
	case pickerProviderKind:
		if err := m.app.SetDefault(ctx, selection.value, ""); err != nil {
			m.append("error", fmt.Sprintf("Error: %v", err))
			return
		}
		appConfig, err := m.app.LoadConfig(ctx)
		if err != nil {
			m.append("error", fmt.Sprintf("Error: %v", err))
			return
		}
		// Dejar un modelo por defecto válido del nuevo proveedor.
		if provider, ok := appConfig.FindProvider(selection.value); ok && len(provider.Models) > 0 {
			_ = m.app.SetDefault(ctx, selection.value, provider.Models[0])
		}
		m.applyConfig(appConfig)
		m.append("notice", fmt.Sprintf("Proveedor por defecto: %s", selection.value))
		// Encadenar al selector de modelos del nuevo proveedor.
		updated, _ := m.app.LoadConfig(ctx)
		if provider, ok := updated.FindProvider(selection.value); ok {
			m.picker = m.modelPickerFor(provider)
		}

	case pickerModelKind:
		if err := m.app.SetDefault(ctx, "", selection.value); err != nil {
			m.append("error", fmt.Sprintf("Error: %v", err))
			return
		}
		appConfig, _ := m.app.LoadConfig(ctx)
		m.applyConfig(appConfig)
		m.append("notice", fmt.Sprintf("Modelo por defecto: %s", selection.value))

	case pickerSessionKind:
		m.sessionID = selection.value
		appConfig, _ := m.app.LoadConfig(ctx)
		m.applyConfig(appConfig)
		m.append("notice", fmt.Sprintf("Sesión %s cargada. Escribe tu prompt para continuar.", selection.value))
	}
}

// permissionDetail devuelve una descripción legible y completa de un tool call
// para mostrarla cuando el usuario pulsa '?' durante un prompt de permiso.
func permissionDetail(call domain.ToolCall) string {
	if len(call.Arguments) == 0 {
		return fmt.Sprintf("Permiso solicitado para: %s", call.Name)
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "Permiso solicitado para: %s\n", call.Name)
	keys := make([]string, 0, len(call.Arguments))
	for key := range call.Arguments {
		keys = append(keys, key)
	}
	// Orden estable para legibilidad.
	sortStrings(keys)
	for _, key := range keys {
		value := call.Arguments[key]
		fmt.Fprintf(&builder, "  %s = %v\n", key, value)
	}
	return strings.TrimRight(builder.String(), "\n")
}

func sortStrings(items []string) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j] < items[j-1]; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

// toggleAgent alterna entre build y plan. El cambio se refleja en la barra de
// estado (agente:X), por lo que no se anexa ninguna línea al transcript.
func (m *Model) toggleAgent() {
	agents := []string{"build", "plan"}
	for index, name := range agents {
		if name == m.agentName {
			m.agentName = agents[(index+1)%len(agents)]
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
	if m.wizard != nil {
		return m.wizard.View()
	}
	if m.picker != nil {
		return m.picker.View()
	}
	if m.helpOpen {
		return m.renderHelp()
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

// renderTranscript dibuja la conversación con word-wrap y scroll (PgUp/PgDn).
// Sin scroll, solo se muestran las últimas 'limit' líneas envueltas.
func (m Model) renderTranscript(limit int) string {
	wrapped := m.wrappedLines()

	if len(wrapped) == 0 {
		return m.styles.dim.Render(" (sin conversación) ")
	}

	maxOffset := len(wrapped) - limit
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
	start := maxOffset - m.scrollOffset
	visible := wrapped[start:]
	return strings.Join(visible, "\n")
}

// wrappedLines devuelve el transcript con cada línea envuelta al ancho y añade
// el buffer "vivo" del asistente para que el streaming se vea en tiempo real.
func (m Model) wrappedLines() []string {
	width := m.width - 2
	if width < 20 {
		width = 80
	}
	var lines []string
	for _, line := range m.transcript {
		lines = append(lines, m.wrap(width, line.kind, line.text)...)
	}
	if m.assistantBuffer != "" {
		lines = append(lines, m.wrap(width, "assistant", m.assistantBuffer)...)
	}
	return lines
}

func (m Model) wrap(width int, kind, text string) []string {
	styled := m.styles.forKind(kind).MaxWidth(width).Render(text)
	return strings.Split(styled, "\n")
}

// renderHelp muestra el overlay de ayuda en pantalla completa.
func (m Model) renderHelp() string {
	lines := []string{
		m.styles.accent.Render("forgen — ayuda rápida"),
		"",
		"Comandos slash (escribe / y Enter en el campo):",
		"  /init       Configura tu proveedor y API key",
		"  /provider   Cambia el proveedor por defecto",
		"  /model      Elige el modelo por defecto (listado en vivo)",
		"  /sessions   Retoma una sesión guardada",
		"  /help, /?   Muestra esta ayuda",
		"  /quit, /exit  Sale de forgen",
		"",
		"Atajos de teclado:",
		"  Enter       Envía el mensaje",
		"  Tab         Cambia agente (build ↔ plan)",
		"  ?           Abre esta ayuda",
		"  PgUp/PgDn   Desplazan la conversación",
		"  Ctrl+C      Cancela la petición en curso / sale",
		"",
		"Consejo: la primera vez escribe /init para conectar tu proveedor favorito",
		"(OpenAI, Anthropic, OpenRouter, Groq, Ollama y más). Solo necesitas tu API key.",
		"",
		m.styles.dim.Render("(Esc o q para cerrar)"),
	}
	return strings.Join(lines, "\n")
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
		right = m.styles.notice.Render(fmt.Sprintf("¿Permitir %s? (y/n, ? ver detalle)", toolCallLabel(m.confirmCall)))
	} else if m.running {
		right = m.styles.dim.Render("trabajando...")
	} else if m.noConfig {
		right = m.styles.notice.Render("sin configurar — escribe /init")
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
		return m.styles.notice.Render(fmt.Sprintf("❯ ¿Permitir ejecutar %s? [y/N, ? detalle]", toolCallLabel(m.confirmCall)))
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
