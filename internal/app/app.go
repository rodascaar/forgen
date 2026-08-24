// Package app es el composition root de forgen: construye el grafo de
// dependencias con DI manual. Es importado por los adapters de entrada.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/rodascaar/forgen/internal/adapters/out/credentials"
	"github.com/rodascaar/forgen/internal/adapters/out/exec"
	"github.com/rodascaar/forgen/internal/adapters/out/fs"
	gitadapter "github.com/rodascaar/forgen/internal/adapters/out/git"
	"github.com/rodascaar/forgen/internal/adapters/out/hook"
	"github.com/rodascaar/forgen/internal/adapters/out/language"
	"github.com/rodascaar/forgen/internal/adapters/out/llm"
	lspadapter "github.com/rodascaar/forgen/internal/adapters/out/lsp"
	"github.com/rodascaar/forgen/internal/adapters/out/sandbox"
	"github.com/rodascaar/forgen/internal/adapters/out/search"
	"github.com/rodascaar/forgen/internal/adapters/out/storage"
	taskadapter "github.com/rodascaar/forgen/internal/adapters/out/task"
	"github.com/rodascaar/forgen/internal/application/agent"
	"github.com/rodascaar/forgen/internal/application/config"
	"github.com/rodascaar/forgen/internal/application/ferment"
	"github.com/rodascaar/forgen/internal/application/lsp"
	"github.com/rodascaar/forgen/internal/application/mcp"
	"github.com/rodascaar/forgen/internal/application/orchestration"
	"github.com/rodascaar/forgen/internal/application/permission"
	"github.com/rodascaar/forgen/internal/application/session"
	"github.com/rodascaar/forgen/internal/application/skills"
	apptask "github.com/rodascaar/forgen/internal/application/task"
	apptodo "github.com/rodascaar/forgen/internal/application/todo"
	"github.com/rodascaar/forgen/internal/application/tools"
	"github.com/rodascaar/forgen/internal/application/usage"
	"github.com/rodascaar/forgen/internal/application/web"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// App es el composition root: construye el grafo de dependencias con DI manual.
type App struct {
	Paths           Paths
	ConfigService   *config.Service
	SessionService  *session.Service
	FermentService  *ferment.Service
	TodoStore       ports.TodoStore
	TaskStore       ports.TaskStore
	Checkpoints     ports.CheckpointStore
	TaskExecutor    ports.TaskExecutor
	Git             ports.Git
	UsageService    *usage.Service
	ToolRegistry    *tools.Registry
	FileSystem      ports.FileSystem
	Language        ports.LanguageDetector
	Toolchain       ports.ToolchainProbe
	LLMFactory      *llm.Factory
	Credentials     ports.CredentialStore
	Skills          []skills.Skill
	MCP             *mcp.Manager
	LSP             *lsp.Manager
	Logger          *slog.Logger
	ActiveFermentID string
}

// NewApp construye la aplicación con todos los adapters inyectados.
func NewApp(logger *slog.Logger) (*App, error) {
	paths := ResolvePaths()
	if err := paths.EnsureDirs(); err != nil {
		return nil, fmt.Errorf("crear directorios de forgen: %w", err)
	}

	configStore := storage.NewYAMLConfigStore(paths.ConfigFile)
	configService := config.NewService(configStore, os.Getenv, config.Overrides{})
	credentialStore := credentials.NewStore(paths.CredentialsFile)

	// Config efectiva temprana (ejecución, web, MCP).
	appConfig, configErr := configService.Load(context.Background())
	if configErr != nil {
		appConfig = domain.DefaultAppConfig()
	}

	jsonlStore, err := storage.NewJSONLStore(paths.SessionsDir)
	if err != nil {
		return nil, err
	}
	sessionService := session.NewService(jsonlStore)

	fermentStore, err := storage.NewJSONLFermentStore(paths.FermentsDir)
	if err != nil {
		return nil, err
	}
	fermentService := ferment.NewService(fermentStore)

	usageStore := storage.NewJSONLUsageStore(paths.UsageFile)
	usageService := usage.NewService(usageStore, logger)

	todoStore, err := storage.NewJSONLTodoStore(paths.TodosFile)
	if err != nil {
		return nil, fmt.Errorf("crear todo store: %w", err)
	}
	taskStore, err := storage.NewJSONLTaskStore(paths.TasksFile)
	if err != nil {
		return nil, fmt.Errorf("crear task store: %w", err)
	}
	checkpointStore := storage.NewCheckpointStore(filepath.Join(paths.DataDir, "checkpoints"))
	taskExecutor := taskadapter.NewExecutor(taskadapter.ExecutorDeps{
		LLMFactory: llm.NewFactory(logger), Credentials: credentialStore,
	}, taskStore)

	// Inyectar resolver orquestado para subagentes (evita hardcode openai/gpt-4).
	// Usa el mismo flujo que ResolveRunModel: clasifica prompt y elige tier.
	llmFactory := llm.NewFactory(logger)
	_ = llmFactory // keep reference for closure
	taskExecutor.SetProviderResolver(func(ctx context.Context, task *domain.Task) (ports.LLMProvider, domain.Model, error) {
		// Resolver fuera de App para evitar ciclo: crear orchestrator temporal.
		cfg, err := configService.Load(ctx)
		if err != nil {
			cfg = domain.DefaultAppConfig()
		}
		// providerAPIKey resolver: credentialStore primero, env fallback
		keyResolver := func(pc domain.ProviderConfig) string {
			if credentialStore != nil {
				if secret, err := credentialStore.Get(ctx, ProviderCredentialKey(pc.Name)); err == nil && secret != "" {
					return secret
				}
			}
			return pc.ResolveAPIKey(os.Getenv)
		}
		orch := orchestration.NewOrchestrator(cfg, llmFactory, keyResolver, logger)
		phase := orch.Classify(task.Description)
		model := orch.SelectFor(phase, task.Description)
		provider, err := orch.Provider(ctx, model)
		if err != nil {
			return nil, domain.Model{}, err
		}
		return provider, model, nil
	})

	workspace, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolver workspace: %w", err)
	}

	fileSystem := fs.New(workspace)
	executor := buildExecutor(workspace, appConfig, paths, logger)
	gitCLI := gitadapter.New()

	// LSP: detectar lenguaje y arrancar el language server (si está instalado).
	lspManager := lsp.NewManager(context.Background(), language.NewDetector(), fileSystem, workspace, logger)

	// Si LSP está activo, las escrituras se sincronizan al language server.
	runtimeFS := ports.FileSystem(fileSystem)
	if lspManager != nil {
		runtimeFS = lspadapter.NewSyncingFileSystem(fileSystem, lspManager.Syncer())
	}

	outputLimit := domain.DefaultAppConfig().MaxOutputChars
	registry := tools.NewRegistry(runtimeFS, executor, gitCLI, outputLimit)

	// Registrar herramientas LSP si hay servidor activo.
	if lspManager != nil {
		lspManager.RegisterTools(registry)
	}

	// Descubrir skills (global + proyecto) y exponer read_skill.
	skillDirs := []string{filepath.Join(paths.ConfigDir, "skills"), ".forgen/skills"}
	discoveredSkills, err := skills.Discover(context.Background(), skillDirs, fileSystem)
	if err != nil {
		logger.Warn("no se pudieron descubrir skills", "err", err)
	}
	registry.Register(skills.NewReadSkillTool(func(name string) (skills.Skill, bool) {
		return skills.ResolveSkill(discoveredSkills, name)
	}))

	// Herramientas de planificación y delegación
	registry.Register(apptodo.NewTool(todoStore))
	registry.Register(apptask.NewTool(taskStore, taskExecutor))

	// Inyectar Runner aislado para subagentes con tools filtradas por tipo (kimchi/opencode).
	// Debe ir después de crear registry para capturar FileSystem/ToolRegistry.
	taskExecutor.SetRunnerFactory(func(ctx context.Context, task *domain.Task) (taskadapter.Runner, error) {
		// Resolver modelo/provider orquestado
		cfg, err := configService.Load(ctx)
		if err != nil {
			cfg = domain.DefaultAppConfig()
		}
		keyResolver := func(pc domain.ProviderConfig) string {
			if credentialStore != nil {
				if secret, err := credentialStore.Get(ctx, ProviderCredentialKey(pc.Name)); err == nil && secret != "" {
					return secret
				}
			}
			return pc.ResolveAPIKey(os.Getenv)
		}
		orch := orchestration.NewOrchestrator(cfg, llmFactory, keyResolver, logger)
		phase := orch.Classify(task.Description)
		model := orch.SelectFor(phase, task.Description)
		provider, err := orch.Provider(ctx, model)
		if err != nil {
			return nil, err
		}
		// Agent filtrado por tipo
		agentDef, ok := domain.FindAgent(domain.BuiltinAgents(), string(task.Type))
		if !ok {
			agentDef = domain.BuiltinAgents()[0]
		}
		// Si el subagente define Prompt/Tools, sobreescribir agente visible
		if task.Config.Prompt != "" {
			agentDef.SystemPrompt = task.Config.Prompt
		}
		if len(task.Config.Tools) > 0 {
			agentDef.AllowedTools = task.Config.Tools
		}
		// Messenger no-op + responder que niega confirms interactivos (subagente no pregunta)
		messenger := &noopMessenger{}
		responder := &autoDenyResponder{}
		// Crear permisos en modo auto para subagente (hereda rules filtradas)
		// Se usa App.NewRunner con workspace actual.
		ws := workspace
		// Capturar servicios necesarios (sessionService, usageService, etc.) vía closure
		// Construir Runner manual para respetar AllowedTools del subagente
		return newSubAgentRunner(ctx, provider, model, agentDef, ws, messenger, responder, registry, sessionService, usageService, credentialStore, logger, cfg)
	})

	// Config efectiva (para web search y MCP).
	// (ya cargada arriba; se reutiliza appConfig)

	// Herramientas web (fetch siempre; search según config).
	registry.Register(web.NewWebFetchTool())
	registry.Register(web.NewWebSearchTool(buildSearchProvider(appConfig, logger)))

	// Arrancar servidores MCP (no fatal si alguno falla).
	mcpManager := mcp.NewManager(registry, logger)
	for _, failure := range mcpManager.Start(context.Background(), appConfig.MCPServers) {
		logger.Warn("mcp server no disponible", "err", failure)
	}

	return &App{
		Paths:          paths,
		ConfigService:  configService,
		SessionService: sessionService,
		FermentService: fermentService,
		UsageService:   usageService,
		TodoStore:      todoStore,
		TaskStore:      taskStore,
		Checkpoints:    checkpointStore,
		TaskExecutor:   taskExecutor,
		Git:            gitCLI,
		ToolRegistry:   registry,
		FileSystem:     fileSystem,
		Language:       language.NewDetector(),
		Toolchain:      language.NewToolchainProbe(),
		LLMFactory:     llm.NewFactory(logger),
		Credentials:    credentialStore,
		Skills:         discoveredSkills,
		MCP:            mcpManager,
		LSP:            lspManager,
		Logger:         logger,
	}, nil
}

// LoadConfig devuelve la configuración efectiva.
func (a *App) LoadConfig(ctx context.Context) (domain.AppConfig, error) {
	return a.ConfigService.Load(ctx)
}

// Close libera todos los recursos externos.
func (a *App) Close() {
	if a.MCP != nil {
		a.MCP.Close()
	}
	if a.LSP != nil {
		a.LSP.Close()
	}
	if a.TodoStore != nil {
		if c, ok := a.TodoStore.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}
	if a.TaskStore != nil {
		if c, ok := a.TaskStore.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}
	if a.TaskExecutor != nil {
		if c, ok := a.TaskExecutor.(interface{ Close() error }); ok {
			_ = c.Close()
		}
	}
}

// RunnerDeps agrupa las dependencias para construir un Runner.
type RunnerDeps struct {
	Provider  ports.LLMProvider
	Model     domain.Model
	Agent     domain.Agent
	Messenger ports.Messenger
	Responder ports.PermissionResponder
	Workspace string
	SessionID string
}

// NewRunner construye un Runner completo (permisos + contexto + tools).
func (a *App) NewRunner(ctx context.Context, deps RunnerDeps) (*agent.Runner, error) {
	appConfig, err := a.LoadConfig(ctx)
	if err != nil {
		return nil, err
	}
	a.ToolRegistry.SetOutputLimit(appConfig.MaxOutputChars)

	persistedRules, err := a.loadPersistedRules(ctx)
	if err != nil {
		return nil, err
	}
	decider := permission.NewService(
		domain.PermissionMode(appConfig.Permissions.Mode),
		deps.Workspace,
		appConfig.Permissions.Rules,
		persistedRules,
	)

	systemPrompt := func(ctx context.Context) (string, error) {
		blocks, err := agent.LoadProjectContext(ctx, deps.Workspace, a.FileSystem)
		if err != nil {
			return "", err
		}
		toolchain, err := agent.LoadToolchainContext(ctx, deps.Workspace, a.Language, a.Toolchain)
		if err != nil {
			return "", err
		}
		// Inyectar el proyecto activo (Ferment), si existe.
		fermentBlock := a.activeFermentBlock(ctx)
		if fermentBlock != "" {
			blocks = append(blocks, agent.ContextBlock{Title: "ferment", Content: fermentBlock})
		}
		// Inyectar el catálogo de skills.
		if catalog := skills.Catalog(a.Skills); catalog != "" {
			blocks = append(blocks, agent.ContextBlock{Title: "skills", Content: catalog})
		}
		return agent.ComposeSystemPrompt(deps.Agent.SystemPrompt, blocks, toolchain), nil
	}

	return agent.NewRunner(agent.Options{
		Provider:      deps.Provider,
		Tools:         a.ToolRegistry,
		Decider:       decider,
		Responder:     deps.Responder,
		Messenger:     deps.Messenger,
		Sessions:      a.SessionService,
		SystemPrompt:  systemPrompt,
		Usage:         a.UsageService,
		MaxIterations: appConfig.MaxIterations,
		Logger:        a.Logger,
	})
}

// ValidateProviderKey valida una API key contra el proveedor y devuelve los
// modelos disponibles para la cuenta. Compartido por CLI (auth) y TUI (init).
func (a *App) ValidateProviderKey(ctx context.Context, config domain.ProviderConfig, apiKey string) ([]string, error) {
	provider, err := a.LLMFactory.CreateWithKeyResolver(config, func(domain.ProviderConfig) string {
		return apiKey
	}, nil)
	if err != nil {
		return nil, err
	}
	models, err := provider.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return config.Models, nil
	}
	return models, nil
}

// HasCredential devuelve true si el proveedor tiene una API key disponible
// (almacén seguro o variable de entorno). Usado para detectar primer uso.
func (a *App) HasCredential(config domain.ProviderConfig) bool {
	return a.providerAPIKey(config) != ""
}

// ProviderUsable devuelve true si el proveedor está listo para usar: tiene una
// API key disponible o es un endpoint local (p. ej. Ollama) que no la requiere.
func (a *App) ProviderUsable(config domain.ProviderConfig) bool {
	if a.HasCredential(config) {
		return true
	}
	return isLocalEndpoint(config.BaseURL)
}

func isLocalEndpoint(baseURL string) bool {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	switch parsed.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// AddProvider hace upsert del proveedor, lo deja como default y persiste.
func (a *App) AddProvider(ctx context.Context, provider domain.ProviderConfig, defaultModel string) error {
	appConfig, err := a.ConfigService.Load(ctx)
	if err != nil {
		return err
	}
	appConfig = appConfig.UpsertProvider(provider)
	appConfig.Default.Provider = provider.Name
	if defaultModel != "" {
		appConfig.Default.Model = defaultModel
	}
	if err := appConfig.Validate(); err != nil {
		return err
	}
	return a.ConfigService.Save(ctx, appConfig)
}

// SetDefault persiste el proveedor y/o modelo por defecto. Valores vacíos se
// ignoran (se dejan sin cambiar).
func (a *App) SetDefault(ctx context.Context, provider, model string) error {
	appConfig, err := a.ConfigService.Load(ctx)
	if err != nil {
		return err
	}
	if provider != "" {
		if _, ok := appConfig.FindProvider(provider); !ok {
			return fmt.Errorf("proveedor %q no configurado", provider)
		}
		appConfig.Default.Provider = provider
	}
	if model != "" {
		appConfig.Default.Model = model
	}
	if err := appConfig.Validate(); err != nil {
		return err
	}
	return a.ConfigService.Save(ctx, appConfig)
}

// ListModelsFor devuelve los modelos disponibles de un proveedor usando su key
// guardada (listado en vivo); si no hay key o falla, devuelve los modelos de la
// config como fallback. Nunca puede quedar vacío si la config define modelos.
func (a *App) ListModelsFor(ctx context.Context, config domain.ProviderConfig) []string {
	apiKey := a.providerAPIKey(config)
	if apiKey == "" {
		return config.Models
	}
	models, err := a.ValidateProviderKey(ctx, config, apiKey)
	if err != nil {
		return config.Models
	}
	if len(models) == 0 {
		return config.Models
	}
	return models
}

// ResolveProvider crea el provider para el modelo configurado.
func (a *App) ResolveProvider(config domain.AppConfig, model domain.Model) (ports.LLMProvider, error) {
	providerConfig, ok := config.FindProvider(model.Provider)
	if !ok {
		return nil, fmt.Errorf("proveedor %q no configurado", model.Provider)
	}
	provider, err := a.LLMFactory.CreateWithKeyResolver(providerConfig, a.providerAPIKey, nil)
	if err != nil {
		return nil, err
	}
	return provider, nil
}

// ProviderCredentialKey devuelve la clave del almacén de credenciales para un
// proveedor. El secreto vive separado de la metadata (config.yaml).
func ProviderCredentialKey(name string) string {
	return "providers/" + name
}

// providerAPIKey resuelve la API key de un proveedor: primero el almacén seguro
// de credenciales, luego la variable de entorno (fallback para CI/entornos).
// Nunca registra ni expone el valor del secreto.
func (a *App) providerAPIKey(config domain.ProviderConfig) string {
	if a.Credentials != nil {
		if secret, err := a.Credentials.Get(context.Background(), ProviderCredentialKey(config.Name)); err == nil && secret != "" {
			return secret
		}
	}
	return config.ResolveAPIKey(os.Getenv)
}

// ResolveRunModel resuelve modelo y provider para un prompt. Si no hay
// override explícito, usa el orquestador multi-modelo (clasificación por fase
// y routing por rol/tier). Devuelve además la fase seleccionada.
func (a *App) ResolveRunModel(ctx context.Context, prompt, overrideProvider, overrideModel string) (domain.Model, ports.LLMProvider, domain.AgentPhase, error) {
	appConfig, err := a.LoadConfig(ctx)
	if err != nil {
		return domain.Model{}, nil, "", err
	}

	if overrideProvider != "" || overrideModel != "" {
		model, err := config.ResolveModel(appConfig, overrideProvider, overrideModel)
		if err != nil {
			return domain.Model{}, nil, "", err
		}
		provider, err := a.ResolveProvider(appConfig, model)
		if err != nil {
			return domain.Model{}, nil, "", err
		}
		return model, provider, domain.PhaseBuild, nil
	}

	orchestrator := orchestration.NewOrchestrator(appConfig, a.LLMFactory, a.providerAPIKey, a.Logger)
	phase := orchestrator.Classify(prompt)
	model := orchestrator.SelectFor(phase, prompt)
	provider, err := orchestrator.Provider(ctx, model)
	if err != nil {
		return domain.Model{}, nil, "", err
	}
	return model, provider, phase, nil
}

// SelectedAgent devuelve el agente configurado o por defecto.
func (a *App) SelectedAgent(appConfig domain.AppConfig, requested string) (domain.Agent, error) {
	name := requested
	if name == "" {
		name = appConfig.Agent
	}
	if name == "" {
		name = "build"
	}
	agents := domain.BuiltinAgents()
	if agentDef, ok := domain.FindAgent(agents, name); ok {
		return agentDef, nil
	}
	return domain.Agent{}, fmt.Errorf("agente %q no encontrado (disponibles: build, plan)", name)
}

// SnapshotWorkspace crea un checkpoint previo a un run del agente (solo build)
// y poda los checkpoints antiguos. Si no hay store, no-op.
func (a *App) SnapshotWorkspace(ctx context.Context, workspace, sessionID string) (domain.Checkpoint, error) {
	if a.Checkpoints == nil {
		return domain.Checkpoint{}, nil
	}
	cp, err := a.Checkpoints.Create(ctx, workspace, sessionID)
	if err == nil {
		_ = a.Checkpoints.Prune(ctx, 10)
	}
	return cp, err
}

// UndoLast revierte la última iteración (checkpoint más reciente) de una sesión.
// Devuelve false si no hay checkpoint disponible.
func (a *App) UndoLast(ctx context.Context, sessionID string) (bool, error) {
	if a.Checkpoints == nil {
		return false, nil
	}
	list, err := a.Checkpoints.List(ctx, sessionID, 1)
	if err != nil || len(list) == 0 {
		return false, err
	}
	if err := a.Checkpoints.Restore(ctx, list[0].ID); err != nil {
		return false, err
	}
	return true, nil
}

// ListCheckpoints devuelve los checkpoints de una sesión.
func (a *App) ListCheckpoints(ctx context.Context, sessionID string, limit int) ([]domain.Checkpoint, error) {
	if a.Checkpoints == nil {
		return nil, nil
	}
	return a.Checkpoints.List(ctx, sessionID, limit)
}

func (a *App) loadPersistedRules(ctx context.Context) ([]domain.PermissionRule, error) {
	store := storage.NewJSONPermissionStore(a.Paths.RulesFile)
	rules, err := store.Load(ctx)
	if err != nil {
		a.Logger.Warn("no se pudieron cargar reglas de permiso persistentes", "err", err)
		return nil, nil
	}
	return rules, nil
}

// activeFermentBlock devuelve el contexto del ferment activo, si lo hay.
func (a *App) activeFermentBlock(ctx context.Context) string {
	if a.ActiveFermentID == "" {
		return ""
	}
	active, err := a.FermentService.Load(ctx, a.ActiveFermentID)
	if err != nil {
		a.Logger.Debug("no se pudo cargar el ferment activo", "id", a.ActiveFermentID, "err", err)
		return ""
	}
	return ferment.ContextBlock(active)
}

// buildSearchProvider construye el proveedor de búsqueda según la config.
func buildSearchProvider(config domain.AppConfig, logger *slog.Logger) ports.SearchProvider {
	switch config.Search.Provider {
	case "brave":
		return search.NewBraveSearch(os.Getenv(config.Search.APIKeyEnv))
	default:
		logger.Debug("búsqueda web deshabilitada (sin search.provider)")
		return nil
	}
}

// subAgentRunner adapta agent.Runner a task.Runner (Run con prompt).
type subAgentRunner struct {
	inner   *agent.Runner
	model   domain.Model
	agent   domain.Agent
	phase   domain.AgentPhase
	session domain.Session
}

func (r *subAgentRunner) Run(ctx context.Context, workspace, prompt string) (string, error) {
	// Crear sesión efímera en memoria (no persiste en SessionStore del usuario, usa TaskStore para resultado)
	sess := r.session
	if sess.ID == "" {
		sess = domain.Session{
			ID:        fmt.Sprintf("subagent-%d", time.Now().UnixNano()),
			Workspace: workspace,
			Model:     r.model,
			Agent:     r.agent.Name,
			StartedAt: time.Now(),
		}
	}
	result, err := r.inner.Run(ctx, agent.RunInput{
		Session:    sess,
		Agent:      r.agent,
		Workspace:  workspace,
		UserPrompt: prompt,
		Phase:      r.phase,
	})
	if err != nil {
		return "", err
	}
	return result.FinalText, nil
}

func newSubAgentRunner(ctx context.Context, provider ports.LLMProvider, model domain.Model, agentDef domain.Agent, workspace string, messenger ports.Messenger, responder ports.PermissionResponder, registry *tools.Registry, sessionService *session.Service, usageService *usage.Service, credStore ports.CredentialStore, logger *slog.Logger, cfg domain.AppConfig) (*subAgentRunner, error) {
	// Permisos auto para subagente (hereda reglas globales, niega interactivo)
	decider := permission.NewService(domain.PermissionModeAuto, workspace, cfg.Permissions.Rules, nil)
	systemPrompt := func(ctx context.Context) (string, error) {
		return agentDef.SystemPrompt, nil
	}
	runner, err := agent.NewRunner(agent.Options{
		Provider:      provider,
		Tools:         registry,
		Decider:       decider,
		Responder:     responder,
		Messenger:     messenger,
		Sessions:      sessionService,
		SystemPrompt:  systemPrompt,
		Usage:         usageService,
		MaxIterations: taskMaxTurns(agentDef, cfg),
		Logger:        logger,
	})
	if err != nil {
		return nil, err
	}
	return &subAgentRunner{
		inner: runner,
		model: model,
		agent: agentDef,
		phase: domain.AgentPhase(agentDef.Name),
	}, nil
}

func taskMaxTurns(agentDef domain.Agent, cfg domain.AppConfig) int {
	if cfg.MaxIterations > 0 {
		return cfg.MaxIterations
	}
	return 30
}

// noopMessenger ignora todos los eventos (subagente no necesita TUI).
type noopMessenger struct{}

func (n *noopMessenger) StreamText(_, _ string)                                        {}
func (n *noopMessenger) ToolStarted(_ string, _ domain.ToolCall)                       {}
func (n *noopMessenger) ToolFinished(_ string, _ domain.ToolCall, _ domain.ToolResult) {}
func (n *noopMessenger) Notice(_ string, _ string)                                     {}
func (n *noopMessenger) Error(_ string, _ error)                                       {}
func (n *noopMessenger) Finished(_ string, _ string)                                   {}

// autoDenyResponder niega cualquier confirmación interactiva en subagente.
type autoDenyResponder struct{}

func (a *autoDenyResponder) Confirm(_ context.Context, _ string, _ domain.ToolCall) (bool, error) {
	return false, nil
}
func (a *autoDenyResponder) Remember(_ context.Context, _ string, _ domain.ToolCall, _ domain.PermissionLevel) error {
	return nil
}

// buildExecutor construye la cadena de ejecución: local o docker, con hooks.
func buildExecutor(workspace string, config domain.AppConfig, paths Paths, logger *slog.Logger) ports.Executor {
	var base ports.Executor = exec.New(workspace)

	if config.Execution.Sandbox == "docker" {
		image := config.Execution.DockerImage
		if image == "" {
			image = "forgen-sandbox"
		}
		base = sandbox.NewDockerExecutor(image, workspace)
	}

	hookDirs := []string{
		filepath.Join(paths.ConfigDir, "hooks", "bash"),
		".forgen/hooks/bash",
	}
	return hook.NewExecutor(base, hookDirs, logger)
}
