package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/common-nighthawk/go-figure"
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
Atajos: Enter envía · Tab cambia agente · PgUp/PgDn o rueda del ratón desplazan · Ctrl+C cancela / salir (2×) · /todo /mcp /help`

// grsprkLogo es la identidad ASCII de forgen (fuente block de go-figure, solo
// ASCII: sin caracteres box-drawing que causaban artefactos/desalineación). Se
// muestra en el banner de inicio y en la ayuda, en el color de marca Lima ácida.
var grsprkLogo = renderForgenLogo()

// renderForgenLogo genera el logotipo FORGEN con la librería go-figure (fuente
// "block"). Se calcula una vez en init y se reutiliza.
func renderForgenLogo() string {
	f := figure.NewFigure("FORGEN", "block", true)
	return strings.Trim(f.String(), "\n")
}

// quitConfirmMsg resetea la confirmación de salida tras un timeout.
type quitConfirmMsg struct{}

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
	reasoning       string
	phase           string
	sessionID       string
	workspace       string
	spinnerIndex    int
	showTodo        bool
	todoList        *domain.TodoList
	todoCursor      int
	showMCP         bool
	mcpCursor       int
	width           int
	height          int
	quitting        bool
	cancelRun       context.CancelFunc
	cancelRequested bool
	noConfig        bool
	scrollOffset    int
	wizard          *wizardModel
	picker          *pickerModel
	helpOpen        bool
	quitArmed       bool
}

// Run inicia la TUI en modo pantalla alternativa.
func Run(app *apppkg.App) error {
	model := newModel(app)
	// Se pasa el modelo como puntero para que 'model.program' (seteado tras
	// NewProgram) sea visible para el modelo del programa. Si se pasa por
	// valor, el programa mantiene una copia con program == nil y el primer
	// streaming del agente crashea (nil deref en tuiMessenger.StreamText).
	program := tea.NewProgram(&model, tea.WithAltScreen(), tea.WithMouseCellMotion())
	model.program = program
	_, err := program.Run()
	return err
}

// newModel construye el estado inicial cargando la configuración. Se construye
// y devuelve el valor completo, por lo que los mensajes de onboarding persisten
// (a diferencia de Init(), que solo devuelve el Cmd y descarta mutaciones).
func newModel(app *apppkg.App) Model {
	input := textinput.New()
	input.Placeholder = "Describe tu tarea... (/ comandos · Ctrl+H ayuda · Enter envía)"
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
	// Identidad: logotipo en color de marca al arrancar la TUI.
	m.append("logo", grsprkLogo)
	m.append("notice", "forgen — agente de desarrollo · plan investiga (no modifica) · build implementa")

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

func (m *Model) loadTodoList() {
	list, err := m.app.TodoStore.Load(context.Background(), "default")
	if err != nil {
		m.todoList = nil
		return
	}
	m.todoList = list
}

func (m Model) renderTodoOverlay() string {
	var b strings.Builder
	b.WriteString(m.styles.accent.Render("Plan — lista de tareas") + "\n\n")
	if m.todoList == nil || len(m.todoList.Todos) == 0 {
		b.WriteString(m.styles.dim.Render("(sin tareas — el agente las crea con todowrite)") + "\n")
	} else {
		d, tot := m.todoList.Progress()
		fmt.Fprintf(&b, "%s\n\n", m.styles.dim.Render(fmt.Sprintf("%d/%d (%.0f%%)", d, tot, m.todoList.ProgressPercent())))
		for i, t := range m.todoList.Todos {
			icon := "○"
			switch t.Status {
			case domain.TodoStatusDone:
				icon = "✓"
			case domain.TodoStatusInProgress:
				icon = "▸"
			case domain.TodoStatusCancelled:
				icon = "✗"
			}
			marker := "  "
			if i == m.todoCursor {
				marker = "▸ "
			}
			line := fmt.Sprintf("%s%d. %s %s", marker, i+1, icon, t.Content)
			if t.Status == domain.TodoStatusInProgress && t.ActiveForm != "" {
				line += " — " + t.ActiveForm
			}
			style := m.styles.dim
			switch t.Status {
			case domain.TodoStatusInProgress:
				style = m.styles.accent
			case domain.TodoStatusDone:
				style = m.styles.toolDone
			}
			if i == m.todoCursor {
				line = m.styles.accent.Render(line)
			} else {
				line = style.Render(line)
			}
			b.WriteString(line + "\n")
		}
	}
	b.WriteString("\n" + m.styles.dim.Render("(↑/↓ mover · Enter/x toggle · d borrar · Esc/q cerrar · Ctrl+P cierra)"))
	return b.String()
}

func (m Model) renderMCPOverlay() string {
	var b strings.Builder
	b.WriteString(m.styles.accent.Render("MCP — servidores") + "\n\n")
	appConfig, err := m.app.LoadConfig(context.Background())
	if err != nil {
		b.WriteString(m.styles.err.Render(fmt.Sprintf("error cargando config: %v", err)) + "\n")
		return b.String()
	}
	if len(appConfig.MCPServers) == 0 {
		b.WriteString(m.styles.dim.Render("(sin servidores — forgen mcp add <nombre>)") + "\n")
		b.WriteString(m.styles.dim.Render("migrate: forgen mcp migrate  · test: forgen mcp test <nombre>") + "\n")
	} else {
		// Lista estable para cursor
		names := make([]string, 0, len(appConfig.MCPServers))
		for name := range appConfig.MCPServers {
			names = append(names, name)
		}
		sortStrings(names)
		for i, name := range names {
			srv := appConfig.MCPServers[name]
			typ := srv.MCPServerType()
			target := srv.Command
			if srv.URL != "" {
				target = srv.URL
			}
			if target == "" {
				target = "(sin comando/url)"
			}
			marker := "  "
			if i == m.mcpCursor {
				marker = "▸ "
			}
			line := fmt.Sprintf("%s%s  %-6s  %s", marker, name, typ, target)
			if i == m.mcpCursor {
				line = m.styles.accent.Render(line)
			} else {
				line = m.styles.toolDone.Render(line)
			}
			b.WriteString(line + "\n")
		}
	}
	// Tools registradas dinámicamente
	if m.app.ToolRegistry != nil {
		tools := m.app.ToolRegistry.ListTools()
		count := 0
		for _, t := range tools {
			if len(t.Name) > 4 && (contains(t.Name, "_")) {
				// heurística: tools mcp contienen "_" prefijo servidor
				count++
			}
		}
		b.WriteString("\n" + m.styles.dim.Render(fmt.Sprintf("tools registradas: %d (mcp_* incluidas)", count)))
	}
	b.WriteString("\n\n" + m.styles.dim.Render("Comandos: forgen mcp add <nombre> --type stdio --command npx  ·  --type http --url https://..."))
	b.WriteString("\n" + m.styles.dim.Render("(↑/↓ mover · Esc/q cerrar · Ctrl+M cierra · /mcp alterna)"))
	return b.String()
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && (s[0:len(substr)] == substr || contains(s[1:], substr)))
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

	case tea.MouseMsg:
		return m.handleMouse(typedMessage)

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
		m.cancelRequested = false
		return m, nil

	case finishedMsg:
		m.flushAssistant()
		m.resetConfirm()
		m.running = false
		m.cancelRequested = false
		m.scrollOffset = 0
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
		m.cancelRequested = false
		m.scrollOffset = 0
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

	case quitConfirmMsg:
		m.quitArmed = false
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

// handleMouse desplaza el transcript con la rueda del ratón / trackpad.
func (m Model) handleMouse(message tea.MouseMsg) (tea.Model, tea.Cmd) {
	// La rueda solo aplica en la vista principal (no en overlays).
	if m.showTodo || m.showMCP || m.helpOpen || m.wizard != nil || m.picker != nil {
		return m, nil
	}
	switch message.Button {
	case tea.MouseButtonWheelUp:
		m.quitArmed = false
		m.scrollBy(+3)
	case tea.MouseButtonWheelDown:
		m.quitArmed = false
		m.scrollBy(-3)
	}
	return m, nil
}

// scrollBy ajusta el offset de scroll con signo positivo = subir, negativo =
// bajar, y lo acota contra el tamaño real del transcript.
func (m *Model) scrollBy(delta int) {
	m.scrollOffset += delta
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
	limit := m.height - 4
	if limit < 5 {
		limit = 5
	}
	maxOffset := len(m.wrappedLines()) - limit
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}
}

func (m Model) handleKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Overlay de MCP: navegación simple — cuchara: solo cierres estándar
	if m.showMCP {
		switch message.String() {
		case "esc", "q", "ctrl+c":
			m.showMCP = false
			m.quitArmed = false
		case "up", "k":
			if m.mcpCursor > 0 {
				m.mcpCursor--
			}
		case "down", "j":
			appConfig, _ := m.app.LoadConfig(context.Background())
			if m.mcpCursor < len(appConfig.MCPServers)-1 {
				m.mcpCursor++
			}
		}
		return m, nil
	}
	// Overlay de todo: navegación y toggle — cuchara: solo cierres estándar
	if m.showTodo {
		switch message.String() {
		case "esc", "q", "ctrl+c":
			m.showTodo = false
			m.quitArmed = false
		case "up", "k":
			if m.todoCursor > 0 {
				m.todoCursor--
			}
		case "down", "j":
			if m.todoList != nil && m.todoCursor < len(m.todoList.Todos)-1 {
				m.todoCursor++
			}
		case "enter", " ", "x":
			if m.todoList != nil && len(m.todoList.Todos) > 0 {
				t := m.todoList.Todos[m.todoCursor]
				if t.Status == domain.TodoStatusDone {
					t.Status = domain.TodoStatusPending
					t.CompletedAt = nil
				} else {
					t.MarkDone()
				}
				_ = m.app.TodoStore.Save(context.Background(), m.todoList)
			}
		case "d":
			if m.todoList != nil && len(m.todoList.Todos) > 0 {
				id := m.todoList.Todos[m.todoCursor].ID
				m.todoList.RemoveTodo(id)
				_ = m.app.TodoStore.Save(context.Background(), m.todoList)
				if m.todoCursor >= len(m.todoList.Todos) && m.todoCursor > 0 {
					m.todoCursor--
				}
			}
		}
		return m, nil
	}
	if m.helpOpen {
		switch message.String() {
		case "esc", "q", "ctrl+c", "ctrl+h":
			m.helpOpen = false
			m.quitArmed = false
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
	case "ctrl+c", "alt+c":
		if m.running {
			// Cancelación idempotente: se pide una sola vez y no se acumulan
			// mensajes ni se repite el cancel mientras el run termina de soltar.
			if !m.cancelRequested {
				m.cancelRequested = true
				if m.cancelRun != nil {
					m.cancelRun()
				}
				m.append("notice", "Cancelando petición...")
			}
			m.quitArmed = false
			return m, nil
		}
		if m.quitArmed {
			m.quitting = true
			return m, tea.Quit
		}
		m.quitArmed = true
		m.append("notice", "¿Salir? Pulsa Ctrl+C de nuevo para confirmar · Esc cancela")
		return m, tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return quitConfirmMsg{} })

	case "esc":
		// Esc cancela confirmación de salida y no interfiere con escritura
		if m.quitArmed {
			m.quitArmed = false
			m.append("notice", "Salida cancelada")
			return m, nil
		}
		return m, nil

	case "pgup":
		m.quitArmed = false
		m.scrollBy(m.height - 4)
		return m, nil

	case "pgdown":
		m.quitArmed = false
		m.scrollBy(-(m.height - 4))
		return m, nil

	case "tab":
		m.quitArmed = false
		if m.running {
			return m, nil
		}
		m.toggleAgent()
		return m, nil

	case "enter":
		m.quitArmed = false
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
		m.cancelRequested = false
		ctx, cancel := context.WithCancel(context.Background())
		m.cancelRun = cancel
		return m, m.startRun(ctx, prompt)

	default:
		// Cualquier tecla no reconocida como atajo desarma el quit y cae al input.
		// Letras como q/p/m/? ahora escriben sin abrir menús — principio cuchara.
		m.quitArmed = false
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
	case "/new":
		m.startNewSession()
		return m, nil
	case "/todo", "/plan":
		m.loadTodoList()
		m.todoCursor = 0
		m.showTodo = true
		return m, nil
	case "/task":
		return m.openTaskPicker()
	case "/mcp":
		m.mcpCursor = 0
		m.showMCP = true
		return m, nil
	case "/diff":
		diff, _ := m.app.Git.Diff(context.Background(), ".", false)
		if diff == "" {
			m.append("notice", "(sin cambios)")
		} else {
			m.append("notice", diff)
		}
		return m, nil
	case "/commit":
		diff, _ := m.app.Git.Diff(context.Background(), ".", true)
		if diff == "" {
			diff, _ = m.app.Git.Diff(context.Background(), ".", false)
		}
		if diff == "" {
			m.append("notice", "(sin cambios para commit)")
		} else {
			m.append("notice", "Diff para commit:\n"+diff[:min(2000, len(diff))])
		}
		return m, nil
	case "/review":
		m.append("notice", "Iniciando review — delegando a sub-agente review…")
		ctx, cancel := context.WithCancel(context.Background())
		m.running = true
		m.assistantBuffer = ""
		m.cancelRun = cancel
		return m, m.startRun(ctx, "Haz un code review del diff actual: busca bugs, seguridad, estilo y sugiere mejoras")
	case "/pr":
		m.append("notice", "Creando PR — ejecuta 'gh pr create' o 'git push && gh pr create'")
		return m, nil
	case "/test":
		m.append("notice", "Ejecutando tests…")
		ctx2, cancel2 := context.WithCancel(context.Background())
		m.running = true
		m.assistantBuffer = ""
		m.cancelRun = cancel2
		return m, m.startRun(ctx2, "Ejecuta los tests relevantes (go test ./... -run <relacionado> o npm test) y reporta fallos")
	case "/lint":
		m.append("notice", "Ejecutando linters…")
		ctx3, cancel3 := context.WithCancel(context.Background())
		m.running = true
		m.assistantBuffer = ""
		m.cancelRun = cancel3
		return m, m.startRun(ctx3, "Ejecuta golangci-lint run ./... (o el linter configurado) y reporta issues")
	case "/fix":
		m.append("notice", "Auto-fix — delegando a sub-agente build…")
		ctx4, cancel4 := context.WithCancel(context.Background())
		m.running = true
		m.assistantBuffer = ""
		m.cancelRun = cancel4
		return m, m.startRun(ctx4, "Corrige automáticamente los errores de lint/test y valida con go vet")
	case "/compact":
		if m.sessionID == "" {
			m.append("notice", "Sin sesión activa — nada que compactar.")
			return m, nil
		}
		focus := ""
		if len(fields) > 1 {
			focus = strings.Join(fields[1:], " ")
		}
		m.append("notice", "Compactando sesión... (prune + LLM summary)")
		ctx2, cancel2 := context.WithCancel(context.Background())
		m.running = true
		m.assistantBuffer = ""
		m.cancelRun = cancel2
		return m, m.startCompact(ctx2, focus)
	case "/context":
		if m.sessionID == "" {
			m.transcript = m.transcript[:0]
			m.append("notice", "Sin sesión activa.")
			return m, nil
		}
		sess, err := m.app.SessionService.Resume(context.Background(), m.sessionID)
		if err != nil {
			m.append("error", fmt.Sprintf("context: %v", err))
			return m, nil
		}
		m.append("notice", compactContextLine(sess, m.app))
		return m, nil
	case "/undo":
		if m.sessionID == "" {
			m.append("notice", "Sin sesión activa todavía.")
			return m, nil
		}
		ok, err := m.app.UndoLast(context.Background(), m.sessionID)
		if err != nil {
			m.append("error", fmt.Sprintf("Error al revertir: %v", err))
		} else if ok {
			m.append("tool_done", "✓ Última iteración revertida (rollback interno).")
		} else {
			m.append("notice", "No hay checkpoint para revertir en esta sesión.")
		}
		return m, nil
	case "/reasoning", "/reason":
		level := ""
		if len(fields) > 1 {
			level = strings.ToLower(strings.TrimSpace(fields[1]))
		}
		if !validReasoningLevel(level) {
			m.append("notice", "Nivel inválido. Usa: /reasoning off|low|medium|high")
			return m, nil
		}
		m.reasoning = level
		m.append("notice", fmt.Sprintf("Razonamiento: %s", m.reasoning))
		return m, nil
	case "/copy":
		return m.handleCopy(fields)
	case "/resume":
		if len(fields) > 1 {
			m.sessionID = fields[1]
			m.loadSessionIntoTranscript(fields[1])
			return m, nil
		}
		m.append("notice", "Uso: /resume <sessionID>")
		return m, nil
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
	// Opción al final para arrancar una sesión limpia.
	items = append(items, pickerItem{label: "＋ Nueva sesión", detail: "Empezar una conversación desde cero", value: newSessionSentinel})
	m.picker = newPickerModel(pickerSessionKind, "Retomar una sesión", items, m.styles, m.width, m.height)
	return m, nil
}

// newSessionSentinel es el valor que el picker devuelve para iniciar sesión nueva.
const newSessionSentinel = "__new_session__"

// startNewSession reinicia el estado para crear una sesión nueva: limpia el
// transcript y libera la sesión activa para que el próximo run cree una nueva.
func (m *Model) startNewSession() {
	m.sessionID = ""
	m.transcript = nil
	m.assistantBuffer = ""
	m.scrollOffset = 0
	m.append("notice", "Sesión nueva. Describe tu tarea…")
}

// openTaskPicker abre el selector de sub-agentes.
func (m Model) openTaskPicker() (tea.Model, tea.Cmd) {
	tasks, err := m.app.TaskStore.List(context.Background(), nil)
	if err != nil {
		m.append("error", fmt.Sprintf("Error: %v", err))
		return m, nil
	}
	if len(tasks) == 0 {
		m.append("notice", "No hay tareas. El agente las crea con 'task'.")
		return m, nil
	}
	items := make([]pickerItem, 0, len(tasks))
	for _, task := range tasks {
		detail := string(task.Status) + " · " + string(task.Type)
		items = append(items, pickerItem{label: task.Name, detail: detail, value: task.ID})
	}
	m.picker = newPickerModel(pickerTaskKind, "Tareas (sub-agentes)", items, m.styles, m.width, m.height)
	return m, nil
}

// modelPickerFor construye el selector de modelos de un proveedor dado.
func (m Model) modelPickerFor(providerConfig domain.ProviderConfig) *pickerModel {
	items := make([]pickerItem, 0, len(providerConfig.Models))
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
		if selection.value == newSessionSentinel {
			m.startNewSession()
			return
		}
		m.sessionID = selection.value
		appConfig, _ := m.app.LoadConfig(ctx)
		m.applyConfig(appConfig)
		m.loadSessionIntoTranscript(selection.value)

	case pickerTaskKind:
		m.append("notice", fmt.Sprintf("Tarea: %s", selection.value))
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

func (m Model) startCompact(ctx context.Context, focus string) tea.Cmd {
	return func() tea.Msg {
		sess, err := m.app.SessionService.Resume(ctx, m.sessionID)
		if err != nil {
			return runDoneMsg{err: err}
		}
		workspace := m.workspace
		if sess.Workspace != "" {
			workspace = sess.Workspace
		}
		appConfig, _ := m.app.LoadConfig(ctx)
		model, provider, _, err := m.app.ResolveRunModel(ctx, sess.Summary(), "", "")
		if err != nil {
			model = sess.Model
			if pc, ok := appConfig.FindProvider(model.Provider); ok {
				_ = pc
				provider, _ = m.app.ResolveProvider(appConfig, model)
			}
		}
		if provider == nil {
			return runDoneMsg{err: fmt.Errorf("no hay provider para compactar")}
		}
		agentDef, _ := m.app.SelectedAgent(appConfig, sess.Agent)
		messenger := newTUIMessenger(m.program)
		runner, err := m.app.NewRunner(ctx, apppkg.RunnerDeps{
			Provider: provider, Model: model, Agent: agentDef, Messenger: messenger, Responder: messenger, Workspace: workspace, SessionID: sess.ID,
		})
		if err != nil {
			return runDoneMsg{err: err}
		}
		if err := runner.CompactNow(ctx, &sess, focus); err != nil {
			return runDoneMsg{err: err}
		}
		m.sessionID = sess.ID
		return runDoneMsg{err: nil, sessionID: sess.ID}
	}
}

// compactContextLine formatea /context para TUI.
func compactContextLine(sess domain.Session, app *apppkg.App) string {
	appConfig, _ := app.LoadConfig(context.Background())
	tokens := 0
	for _, msg := range sess.Messages {
		tokens += len(msg.Text())/4 + 4
	}
	limit := 128000
	if md, ok := appConfig.ModelMetadata[sess.Model.Key()]; ok && md.ContextLimit > 0 {
		limit = md.ContextLimit
	}
	pct := float64(tokens) / float64(limit) * 100
	return fmt.Sprintf("Context: %d msgs · %d/%d tokens (%.1f%%) · compactions %d boundary %d summary %d chars", len(sess.Messages), tokens, limit, pct, sess.CompactionCount, sess.CompactBoundary, len(sess.CompactionSummary))
}

// startRun arma el comando del agente en segundo plano + el ticker del spinner.
// El estado m.running ya se activó en handleKey (este método recibe m por valor).
func (m Model) startRun(ctx context.Context, prompt string) tea.Cmd {
	runCommand := func() tea.Msg {
		return runAgent(ctx, m.app, m.sessionID, m.workspace, m.agentName, prompt, m.reasoning, m.program)
	}
	ticker := tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg{} })
	return tea.Batch(runCommand, ticker)
}

// runTimeout acota todo el turno del agente como red de seguridad: si algo
// quedara colgado pese a la cancelación por proceso/LLM, el spinner se apaga.
const runTimeout = 10 * time.Minute

// runAgent ejecuta un turno del agente en segundo plano.
func runAgent(ctx context.Context, app *apppkg.App, sessionID, workspace, agentName, prompt, reasoning string,
	program *tea.Program) tea.Msg {
	// Red de seguridad anti-cuelgue: hereda la cancelación (Ctrl+C) y añade un
	// tope global para que runDoneMsg siempre se emita y el spinner se apague.
	runCtx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	appConfig, err := app.LoadConfig(runCtx)
	if err != nil {
		return runDoneMsg{err: err}
	}

	model, provider, phase, err := app.ResolveRunModel(runCtx, prompt, "", "")
	if err != nil {
		return runDoneMsg{err: err}
	}
	agentDef, err := app.SelectedAgent(appConfig, agentName)
	if err != nil {
		return runDoneMsg{err: err}
	}

	// Primera ejecución: crear la sesión.
	session := loadOrCreateSessionTUI(runCtx, app, sessionID, workspace, model, agentDef.Name)
	// Fix: si la sesión reanudada trae otro modelo/proveedor (p.ej. se cambió con
	// /model), sincronizarla con el modelo resuelto para que el cambio aplique
	// en el mismo turno sin necesidad de reiniciar.
	if session.Model.Key() != model.Key() {
		session.Model = model
		_ = app.SessionService.Save(runCtx, session)
	}
	// Rollback interno: snapshot previo a un run de build (no read-only).
	if !agentDef.IsReadOnly {
		_, _ = app.SnapshotWorkspace(runCtx, workspace, session.ID)
	}
	messenger := newTUIMessenger(program)

	runner, err := app.NewRunner(runCtx, apppkg.RunnerDeps{
		Provider:        provider,
		Model:           model,
		Agent:           agentDef,
		Messenger:       messenger,
		Responder:       messenger,
		Workspace:       workspace,
		SessionID:       session.ID,
		ReasoningEffort: reasoning,
	})
	if err != nil {
		return runDoneMsg{err: err}
	}
	result, err := runner.Run(runCtx, agent.RunInput{
		Session:    session,
		Agent:      agentDef,
		Workspace:  workspace,
		UserPrompt: prompt,
		Phase:      phase,
	})
	if err != nil {
		return runDoneMsg{err: err}
	}
	_ = app.SessionService.Save(runCtx, result.Session)
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
	if m.showMCP {
		return m.renderMCPOverlay()
	}
	if m.showTodo {
		return m.renderTodoOverlay()
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
	end := min(start+limit, len(wrapped))
	visible := wrapped[start:end]
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
		if line.kind == "assistant" && isRecommendation(line.text) {
			// La recomendación del plan se resalta en el color de marca.
			lines = append(lines, strings.Split(m.styles.brand.Render(line.text), "\n")...)
			continue
		}
		lines = append(lines, m.wrap(width, line.kind, line.text)...)
	}
	if m.assistantBuffer != "" {
		lines = append(lines, m.wrap(width, "assistant", m.assistantBuffer)...)
	}
	return lines
}

func (m Model) wrap(width int, kind, text string) []string {
	if kind == "logo" {
		// El logotipo se pinta en el color de marca sin ajuste de línea.
		return m.renderLogoLines()
	}
	styled := m.styles.forKind(kind).MaxWidth(width).Render(text)
	return strings.Split(styled, "\n")
}

// renderLogoLines devuelve el logotipo FORGEN, cada línea con su propio color
// de marca #A6D93B (TrueColor) y RESET al final, para que no haya fugas de
// color ni desalineación entre líneas.
func (m Model) renderLogoLines() []string {
	raw := strings.Split(strings.TrimRight(grsprkLogo, "\n"), "\n")
	out := make([]string, len(raw))
	for i, line := range raw {
		out[i] = m.styles.brand.Render(strings.TrimRight(line, " "))
	}
	return out
}

// renderHelp muestra el overlay de ayuda en pantalla completa.
func (m Model) renderHelp() string {
	lines := make([]string, 0, 24)
	lines = append(lines, m.renderLogoLines()...)
	lines = append(lines,
		m.styles.brand.Render("FORGEN — ayuda rápida"),
		"",
		"Escribir es siempre seguro: ninguna letra sola abre menús. Usa /comandos o Ctrl+atajos.",
		"",
		"Comandos slash (escribe / y Enter en el campo):",
		"  /init       Configura tu proveedor y API key",
		"  /provider   Cambia el proveedor por defecto",
		"  /model      Elige el modelo por defecto (listado en vivo)",
		"  /sessions   Retoma una sesión guardada",
		"  /new        Inicia una sesión nueva",
		"  /todo, /plan Visualiza la lista de tareas (todowrite)",
		"  /task       Lista sub-agentes",
		"  /mcp        MCP servidores",
		"  /diff       Muestra diff del working tree",
		"  /commit     Muestra diff para commit",
		"  /review     Code review del diff (sub-agente)",
		"  /test       Ejecuta tests relevantes",
		"  /lint       Ejecuta linters",
		"  /fix        Auto-fix lint/test",
		"  /pr         Crear PR (gh pr create)",
		"  /reasoning Nivel de razonamiento: off|low|medium|high",
		"  /copy      Copia la última respuesta al portapapeles (/copy all)",
		"  /undo      Revierte la última iteración (checkpoint interno)",
		"  /resume    Reanuda una sesión por ID",
		"  /help, /?   Muestra esta ayuda",
		"  /quit, /exit  Sale de forgen",
		"",
		"Atajos (no requieren Ctrl; las teclas de edición quedan libres):",
		"  Enter        Envía el mensaje",
		"  Tab          Cambia agente (build ↔ plan)",
		"  PgUp / PgDn  Desplazan la conversación",
		"  Rueda ratón  Desplaza la conversación (trackpad también)",
		"  Ctrl+C       Cancela la petición en curso · pulsar 2× para salir",
		"  Esc          Cierra overlays / cancela salida",
		"",
		"Ver también: /todo (plan), /mcp (servidores), /help (esta ayuda).",
		"Las teclas de edición estándar (Ctrl+U/D/H/W/A/E…) funcionan en el campo,",
		"para que los atajos de tu editor y de macOS no se bloqueen.",
		"",
		"Consejo: la primera vez escribe /init para conectar tu proveedor favorito",
		"(OpenAI, Anthropic, OpenRouter, Groq, Ollama y más). Solo necesitas tu API key.",
		"",
		m.styles.dim.Render("(Esc o q para cerrar ayuda)"),
	)
	return strings.Join(lines, "\n")
}

func (m Model) renderStatus() string {
	// Prompt estilo shell con el color de marca forgen: "~/proj $ build"
	prompt := fmt.Sprintf("%s $ %s", m.workspace, m.agentName)
	left := ""
	if m.running {
		left += m.styles.accent.Render(spinnerFrames[m.spinnerIndex])
	} else {
		left += m.styles.dim.Render("●")
	}
	left += " " + m.styles.accent.Render(prompt)
	left += " " + m.styles.dim.Render(fmt.Sprintf("modelo:%s", m.modelKey))
	if m.reasoning != "" && m.reasoning != "off" {
		left += " " + m.styles.accent.Render(fmt.Sprintf("raz:%s", m.reasoning))
	}
	if m.phase != "" {
		left += " " + m.styles.accent.Render(fmt.Sprintf("fase:%s", m.phase))
	}
	if list, err := m.app.TodoStore.Load(context.Background(), "default"); err == nil && len(list.Todos) > 0 {
		d, tot := list.Progress()
		left += " " + m.styles.toolDone.Render(fmt.Sprintf("📋 %d/%d", d, tot))
	}
	right := ""
	if m.confirming {
		right = m.styles.notice.Render(fmt.Sprintf("¿Permitir %s? (y/n, ? ver detalle)", toolCallLabel(m.confirmCall)))
	} else if m.running {
		right = m.styles.dim.Render("trabajando... · Ctrl+C cancela")
	} else if m.quitArmed {
		right = m.styles.notice.Render("¿Salir? Ctrl+C de nuevo · Esc cancela")
	} else if m.noConfig {
		right = m.styles.notice.Render("sin configurar — escribe /init")
	} else {
		right = m.styles.dim.Render("/ comandos · Tab agente · rueda/PgUp desplaza")
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
	// Barra de escritura con borde de marca: siempre clara dónde escribir.
	borderColor := m.styles.accent.GetForeground()
	bar := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		PaddingLeft(1).
		PaddingRight(1)
	if m.width > 2 {
		bar = bar.Width(m.width - 2)
	}
	return bar.Render(m.input.View())
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

// validReasoningLevel indica si un nivel de razonamiento es válido.
func validReasoningLevel(level string) bool {
	switch level {
	case "", "off", "low", "medium", "high":
		return true
	}
	return false
}

// handleCopy copia al portapapeles: la última respuesta del asistente (/copy)
// o todo el transcript (/copy all).
func (m *Model) handleCopy(fields []string) (tea.Model, tea.Cmd) {
	all := len(fields) > 1 && fields[1] == "all"
	var text string
	if all {
		var builder strings.Builder
		for _, line := range m.transcript {
			builder.WriteString(line.text)
			builder.WriteString("\n")
		}
		text = strings.TrimRight(builder.String(), "\n")
	} else {
		text = m.lastAssistantText()
	}
	if text == "" {
		m.append("notice", "Nada que copiar todavía.")
		return m, nil
	}
	if err := clipboard.WriteAll(text); err != nil {
		m.append("error", fmt.Sprintf("Error copiando al portapapeles: %v", err))
		return m, nil
	}
	if all {
		m.append("tool_done", "✓ Transcript copiado al portapapeles.")
	} else {
		m.append("tool_done", "✓ Última respuesta copiada al portapapeles.")
	}
	return m, nil
}

// lastAssistantText devuelve la última respuesta de texto del asistente.
func (m *Model) lastAssistantText() string {
	if m.assistantBuffer != "" {
		return m.assistantBuffer
	}
	for i := len(m.transcript) - 1; i >= 0; i-- {
		if m.transcript[i].kind == "assistant" {
			return m.transcript[i].text
		}
	}
	return ""
}

// loadSessionIntoTranscript carga los mensajes de una sesión en el transcript
// para poder ver y continuar una conversación anterior.
func (m *Model) loadSessionIntoTranscript(sessionID string) {
	session, err := m.app.SessionService.Resume(context.Background(), sessionID)
	if err != nil {
		m.append("error", fmt.Sprintf("Error al resumir sesión: %v", err))
		return
	}
	m.transcript = m.transcript[:0]
	for _, msg := range session.Messages {
		switch msg.Role {
		case domain.RoleUser:
			m.append("user", msg.Text())
		case domain.RoleAssistant:
			if text := msg.Text(); text != "" {
				m.append("assistant", text)
			}
		}
	}
	m.append("notice", fmt.Sprintf("Sesión %s cargada. Escribe para continuar.", sessionID))
}

// isRecommendation indica si una línea del asistente corresponde a la
// recomendación del modo plan (marcada por el agente como "✅ Recomendación:").
// Se resalta en el color de marca en la TUI.
func isRecommendation(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if strings.Contains(lower, "✅ recomendación") {
		return true
	}
	if strings.HasPrefix(lower, "recomendación:") ||
		strings.HasPrefix(lower, "recomendado:") ||
		strings.HasPrefix(lower, "recomendada:") ||
		strings.HasPrefix(lower, "recomendacion:") {
		return true
	}
	return false
}

func summarize(text string) string {
	text = strings.TrimSpace(text)
	if len(text) > 80 {
		return text[:80] + "..."
	}
	return text
}

type tickMsg struct{}
