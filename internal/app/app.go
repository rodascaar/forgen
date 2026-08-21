// Package app es el composition root de forgen: construye el grafo de
// dependencias con DI manual. Es importado por los adapters de entrada.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

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
	"github.com/rodascaar/forgen/internal/application/agent"
	"github.com/rodascaar/forgen/internal/application/config"
	"github.com/rodascaar/forgen/internal/application/ferment"
	"github.com/rodascaar/forgen/internal/application/lsp"
	"github.com/rodascaar/forgen/internal/application/mcp"
	"github.com/rodascaar/forgen/internal/application/orchestration"
	"github.com/rodascaar/forgen/internal/application/permission"
	"github.com/rodascaar/forgen/internal/application/session"
	"github.com/rodascaar/forgen/internal/application/skills"
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

// Close libera los recursos externos (servidores MCP y LSP).
func (a *App) Close() {
	if a.MCP != nil {
		a.MCP.Close()
	}
	if a.LSP != nil {
		a.LSP.Close()
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
