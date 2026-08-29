// Package app es el composition root de forgen: construye el grafo de
// dependencias con DI manual. Es importado por los adapters de entrada.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
	"github.com/rodascaar/forgen/internal/application/memory"
	"github.com/rodascaar/forgen/internal/application/orchestration"
	"github.com/rodascaar/forgen/internal/application/permission"
	appplan "github.com/rodascaar/forgen/internal/application/plan"
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
	gitCLI := gitadapter.NewCombined(gitadapter.New(), filepath.Join(paths.DataDir, "workspaces"))

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
	registry.Register(appplan.NewTool(todoStore))
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
		messenger := &noopMessenger{}
		responder := &autoDenyResponder{}
		ws := workspace
		// Fresh window con App context (AGENTS.md, toolchain, skills)
		tmpApp := &App{
			FileSystem:      fileSystem,
			Language:        language.NewDetector(),
			Toolchain:       language.NewToolchainProbe(),
			Skills:          discoveredSkills,
			Logger:          logger,
			ActiveFermentID: "",
			ToolRegistry:    registry,
			SessionService:  sessionService,
		}
		// Cargar config actual para Compaction + lenguaje
		return tmpApp.newSubAgentRunner(ctx, provider, model, agentDef, ws, messenger, responder, cfg)
	})

	// Config efectiva (para web search y MCP).
	// (ya cargada arriba; se reutiliza appConfig)

	// Herramientas web (fetch siempre; search según config).
	registry.Register(web.NewWebFetchTool())
	registry.Register(web.NewWebSearchTool(buildSearchProvider(appConfig, credentialStore, logger)))

	// Arrancar servidores MCP (no fatal si alguno falla).
	mcpManager := mcp.NewManager(registry, logger)
	if err := mcpManager.Start(context.Background(), appConfig.MCPServers); err != nil {
		// Iterar sobre la cadena de errores unidos con errors.Join
		var failures []error
		for e := err; e != nil; e = errors.Unwrap(e) {
			failures = append(failures, e)
		}
		// Si no se pudieron separar, usar el error completo
		if len(failures) == 0 {
			failures = append(failures, err)
		}
		for _, failure := range failures {
			logger.Warn("mcp server no disponible", "err", failure)
		}
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
	Provider        ports.LLMProvider
	Model           domain.Model
	Agent           domain.Agent
	Messenger       ports.Messenger
	Responder       ports.PermissionResponder
	Workspace       string
	SessionID       string
	ReasoningEffort string
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

	// Resolver idioma es/en para prompts (config.language → env FORGEN_LANG → en).
	lang := domain.ResolveLanguage(appConfig.Language)
	if lang == "en" {
		if envLang := domain.ResolveLanguage(getEnvLang()); envLang == "es" {
			lang = "es"
		}
	}
	// Si deps.Agent trae prompt vacío, resolver por idioma via PromptFor.
	resolvedAgent := deps.Agent
	if resolvedAgent.SystemPrompt == "" {
		if p := domain.PromptFor(resolvedAgent.Name, lang); p != "" {
			resolvedAgent.SystemPrompt = p
		}
	} else {
		// Si el agente viene de BuiltinAgents default (en), respetar lang override si config pide es.
		if lang == "es" && resolvedAgent.Name == "build" {
			if p := domain.PromptFor("build", "es"); p != "" && resolvedAgent.SystemPrompt != p {
				// Solo sobrescribir si el prompt es el legacy en (no custom de usuario).
				// Heurística: si contiene "You are forgen" en inglés, es legacy.
				if isLegacyEnglishPrompt(resolvedAgent.SystemPrompt) {
					resolvedAgent.SystemPrompt = p
				}
			}
		}
		if lang == "es" && resolvedAgent.Name == "plan" {
			if p := domain.PromptFor("plan", "es"); p != "" && isLegacyEnglishPrompt(resolvedAgent.SystemPrompt) {
				resolvedAgent.SystemPrompt = p
			}
		}
	}
	// Tuning por familia: hint para 9B vs GPT (agnóstico).
	modelFamilyHint := ""
	if isGPTFamily(deps.Model) {
		if lang == "es" {
			modelFamilyHint = "Familia del modelo: GPT/Codex — prefiere `apply_patch` para cambios multi-archivo/revisables; usa `edit` solo para fix quirúrgico de 1 línea."
		} else {
			modelFamilyHint = "Model family: GPT/Codex — prefer `apply_patch` for multi-file/reviewable changes; use `edit` only for 1-line surgical fix."
		}
	} else {
		if lang == "es" {
			modelFamilyHint = "Familia del modelo: ligero/local (9B-12B) — prefiere `edit` y `write` quirúrgicos; evita patches grandes; usa `read_many_files` para batch."
		} else {
			modelFamilyHint = "Model family: lightweight/local (9B-12B) — prefer surgical `edit`/`write`; avoid large patches; use `read_many_files` for batch reads."
		}
	}
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
		// Inyectar memoria workspace .forgen/memory.md (7.6.1)
		if mem := loadMemoryBlock(deps.Workspace); mem != "" {
			blocks = append(blocks, agent.ContextBlock{Title: "memory", Content: mem})
		}
		// Inyectar el catálogo de skills con budget 25k/5k LIFO (7.6.2)
		if catalog := skills.CatalogWithBudget(a.Skills, 25000, 5000); catalog != "" {
			blocks = append(blocks, agent.ContextBlock{Title: "skills", Content: catalog})
		}
		sys := resolvedAgent.SystemPrompt
		if modelFamilyHint != "" {
			sys += "\n\n" + modelFamilyHint
		}
		return agent.ComposeSystemPrompt(sys, blocks, toolchain), nil
	}

	// PostToolUse diagnostics closure (7.5.2) — feed LSP diagnostics after edit/write/patch.
	var diagFn func(context.Context, string) string
	if a.LSP != nil {
		diagFn = func(ctx context.Context, path string) string {
			return a.LSP.DiagnosticsFor(ctx, path)
		}
	}
	return agent.NewRunner(agent.Options{
		Provider:        deps.Provider,
		Tools:           a.ToolRegistry,
		Decider:         decider,
		Responder:       deps.Responder,
		Messenger:       deps.Messenger,
		Sessions:        a.SessionService,
		SystemPrompt:    systemPrompt,
		Usage:           a.UsageService,
		MaxIterations:   appConfig.MaxIterations,
		ReasoningEffort: reasoningEffortOr(deps.ReasoningEffort, appConfig.ReasoningEffort),
		Compaction: agent.CompactionConfig{
			Threshold:     appConfig.Compaction.CompactionThreshold(),
			Disabled:      appConfig.Compaction.Disabled,
			ModelMetadata: appConfig.ModelMetadata,
		},
		Diagnostics: diagFn,
		Logger:      a.Logger,
	})
}

// reasoningEffortOr prioriza el override (flag/comando) sobre el config.
func reasoningEffortOr(override, config string) string {
	if override != "" {
		return override
	}
	return config
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
	case "localhost", "127.0.0.1", "::1", "host.docker.internal", "0.0.0.0", "ollama":
		return true
	}
	host := parsed.Hostname()
	if strings.HasPrefix(host, "192.168.") || strings.HasPrefix(host, "10.") || strings.HasPrefix(host, "127.") {
		return true
	}
	if strings.HasPrefix(host, "172.") {
		// 172.16.0.0/12 -> 172.16-31.*
		parts := strings.Split(host, ".")
		if len(parts) >= 2 {
			var sec int
			if _, err := fmt.Sscanf(parts[1], "%d", &sec); err == nil {
				if sec >= 16 && sec <= 31 {
					return true
				}
			}
		}
	}
	if strings.HasSuffix(host, ".local") {
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

// SetOrchestration persiste el estado del routing automático multi-modelo y el
// pool de modelos elegido. Un pool vacío significa "todos los del proveedor".
func (a *App) SetOrchestration(ctx context.Context, auto bool, pool []string) error {
	config, err := a.ConfigService.Load(ctx)
	if err != nil {
		return err
	}
	config.Orchestration.Auto = auto
	if pool != nil {
		config.Orchestration.Pool = pool
	}
	if err := config.Validate(); err != nil {
		return err
	}
	return a.ConfigService.Save(ctx, config)
}

// OrchestrationModels devuelve los modelos disponibles del proveedor por
// defecto (listado en vivo con la key guardada; fallback a la config).
func (a *App) OrchestrationModels(ctx context.Context) []string {
	config, err := a.ConfigService.Load(ctx)
	if err != nil {
		return nil
	}
	provider, ok := config.FindProvider(config.Default.Provider)
	if !ok {
		return nil
	}
	return a.ListModelsFor(ctx, provider)
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

// SearchCredentialKey devuelve la clave del almacén de credenciales para el
// proveedor de búsqueda web (Brave). Al igual que las API keys de los LLM,
// el secreto se guarda en el credential store y se lee en runtime.
func SearchCredentialKey(provider string) string {
	return "search/" + provider
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
	// Fallback chain: probar pool en orden hasta 3 modelos (como Claude Code fallbackModel)
	pool := orchestrator.PoolForPhase(phase)
	if len(pool) == 0 {
		pool = []domain.Model{model}
	}
	var lastErr error
	for i, candidate := range pool {
		if i >= 3 {
			break
		}
		provider, err := orchestrator.Provider(ctx, candidate)
		if err != nil {
			lastErr = err
			continue
		}
		return candidate, provider, phase, nil
	}
	if lastErr != nil {
		return domain.Model{}, nil, "", lastErr
	}
	provider, err := orchestrator.Provider(ctx, model)
	if err != nil {
		return domain.Model{}, nil, "", err
	}
	return model, provider, phase, nil
}

// SelectedAgent devuelve el agente configurado o por defecto (respeta language es/en).
func (a *App) SelectedAgent(appConfig domain.AppConfig, requested string) (domain.Agent, error) {
	name := requested
	if name == "" {
		name = appConfig.Agent
	}
	if name == "" {
		name = "build"
	}
	lang := domain.ResolveLanguage(appConfig.Language)
	if lang == "en" {
		if envLang := domain.ResolveLanguage(getEnvLang()); envLang == "es" {
			lang = "es"
		}
	}
	agents := domain.BuiltinAgentsForLang(lang)
	if agentDef, ok := domain.FindAgent(agents, name); ok {
		return agentDef, nil
	}
	return domain.Agent{}, fmt.Errorf("agente %q no encontrado (disponibles: build, plan)", name)
}

func getEnvLang() string {
	// FORGEN_LANG o LANG
	if v := os.Getenv("FORGEN_LANG"); v != "" {
		return v
	}
	if v := os.Getenv("LANG"); v != "" {
		if len(v) >= 2 && (v[:2] == "es" || v[:2] == "ES") {
			return "es"
		}
	}
	return "en"
}

func isLegacyEnglishPrompt(p string) bool {
	return strings.Contains(p, "You are forgen") || strings.Contains(p, "You are forgen in plan")
}

func loadMemoryBlock(workspace string) string {
	m := memory.New(workspace)
	if b := m.LoadWorkspace(context.Background()); b != "" {
		return b
	}
	return ""
}

func isGPTFamily(m domain.Model) bool {
	id := strings.ToLower(m.ID)
	prov := strings.ToLower(m.Provider)
	if strings.Contains(id, "gpt") || strings.Contains(id, "codex") || strings.Contains(id, "o1") || strings.Contains(id, "o3") {
		return true
	}
	if prov == "openai" && (strings.Contains(id, "gpt") || strings.Contains(id, "mini") || strings.Contains(id, "nano")) {
		return true
	}
	// openai_compatible with gpt in id still counts
	if strings.Contains(prov, "openai") && strings.Contains(id, "gpt") {
		return true
	}
	return false
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
// La API key se resuelve primero desde el CredentialStore (SearchCredentialKey)
// y luego desde la variable de entorno configurada (fallback para CI).
func buildSearchProvider(config domain.AppConfig, credentials ports.CredentialStore, logger *slog.Logger) ports.SearchProvider {
	switch config.Search.Provider {
	case "brave":
		key := ""
		if credentials != nil {
			if secret, err := credentials.Get(context.Background(), SearchCredentialKey("brave")); err == nil && secret != "" {
				key = secret
			}
		}
		if key == "" {
			key = os.Getenv(config.Search.APIKeyEnv)
		}
		return search.NewBraveSearch(key)
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

func (a *App) newSubAgentRunner(ctx context.Context, provider ports.LLMProvider, model domain.Model, agentDef domain.Agent, workspace string, messenger ports.Messenger, responder ports.PermissionResponder, cfg domain.AppConfig) (*subAgentRunner, error) {
	decider := permission.NewService(domain.PermissionModeAuto, workspace, cfg.Permissions.Rules, nil)
	systemPrompt := func(ctx context.Context) (string, error) {
		blocks, err := agent.LoadProjectContext(ctx, workspace, a.FileSystem)
		if err != nil {
			return agentDef.SystemPrompt, nil
		}
		toolchain, _ := agent.LoadToolchainContext(ctx, workspace, a.Language, a.Toolchain)
		fermentBlock := a.activeFermentBlock(ctx)
		if fermentBlock != "" {
			blocks = append(blocks, agent.ContextBlock{Title: "ferment", Content: fermentBlock})
		}
		if catalog := skills.Catalog(a.Skills); catalog != "" {
			blocks = append(blocks, agent.ContextBlock{Title: "skills", Content: catalog})
		}
		return agent.ComposeSystemPrompt(agentDef.SystemPrompt, blocks, toolchain), nil
	}
	runner, err := agent.NewRunner(agent.Options{
		Provider:      provider,
		Tools:         a.ToolRegistry,
		Decider:       decider,
		Responder:     responder,
		Messenger:     messenger,
		Sessions:      a.SessionService,
		SystemPrompt:  systemPrompt,
		Usage:         a.UsageService,
		MaxIterations: taskMaxTurns(agentDef, cfg),
		Compaction: agent.CompactionConfig{
			Threshold:     cfg.Compaction.CompactionThreshold(),
			Disabled:      cfg.Compaction.Disabled,
			ModelMetadata: cfg.ModelMetadata,
		},
		Logger: a.Logger,
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

func (a *autoDenyResponder) Confirm(_ context.Context, _ string, _ domain.ToolCall) (domain.PermissionChoice, error) {
	return domain.ChoiceDeny(), nil
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
