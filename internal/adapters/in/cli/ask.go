package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/rodascaar/forgen/internal/adapters/in/tui"
	apppkg "github.com/rodascaar/forgen/internal/app"
	"github.com/rodascaar/forgen/internal/application/agent"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/spf13/cobra"
)

// askOptions son los flags del comando ask.
type askOptions struct {
	Provider       string
	Model          string
	Agent          string
	PermissionMode string
	SessionID      string
	JSON           bool
	Prompt         string
}

func newAskCommand(app *apppkg.App) *cobra.Command {
	options := &askOptions{}
	command := &cobra.Command{
		Use:   "ask [prompt]",
		Short: "Ejecuta una petición única (headless)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				options.Prompt = strings.TrimSpace(strings.Join(args, " "))
			}
			if options.Prompt == "" {
				return fmt.Errorf("proporciona un prompt (texto o --prompt)")
			}
			return runAsk(cmd.Context(), app, options)
		},
	}
	command.Flags().StringVar(&options.Provider, "provider", "", "Proveedor LLM (override de config)")
	command.Flags().StringVar(&options.Model, "model", "", "Modelo LLM (override de config)")
	command.Flags().StringVar(&options.Agent, "agent", "", "Agente a usar (build | plan)")
	command.Flags().StringVar(&options.PermissionMode, "permission-mode", "", "Modo de permisos (auto | on_request | never)")
	command.Flags().StringVar(&options.SessionID, "session", "", "ID de sesión a resumir")
	command.Flags().BoolVar(&options.JSON, "json", false, "Salida JSON estructurada (eventos)")
	command.Flags().StringVarP(&options.Prompt, "prompt", "m", "", "Prompt a ejecutar")
	return command
}

func runAsk(ctx context.Context, app *apppkg.App, options *askOptions) error {
	appConfig, err := app.ConfigService.Load(ctx)
	if err != nil {
		return err
	}
	if options.PermissionMode != "" {
		appConfig.Permissions.Mode = options.PermissionMode
	}
	if options.Agent != "" {
		appConfig.Agent = options.Agent
	}

	// Resolver modelo y provider: override explícito o orquestación multi-modelo.
	model, provider, phase, err := app.ResolveRunModel(ctx, options.Prompt, options.Provider, options.Model)
	if err != nil {
		return err
	}
	agentDef, err := app.SelectedAgent(appConfig, options.Agent)
	if err != nil {
		return err
	}

	workspace, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolver workspace: %w", err)
	}

	// Sesión: resumir la indicada o crear una nueva.
	session, err := loadOrCreateSession(ctx, app, options.SessionID, workspace, model, agentDef.Name)
	if err != nil {
		return err
	}
	defer func() { _ = app.SessionService.Save(ctx, session) }()

	var messenger *textMessenger
	if options.JSON {
		messenger = newJSONMessenger(os.Stdout, os.Stdin)
	} else {
		messenger = newTextMessenger(os.Stdout, os.Stdin)
	}
	app.Logger.Info("agent.run.start", "session", session.ID, "phase", phase, "model", model.Key())

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
		return err
	}

	result, err := runner.Run(ctx, agent.RunInput{
		Session:    session,
		Agent:      agentDef,
		Workspace:  workspace,
		UserPrompt: options.Prompt,
		Phase:      phase,
	})
	if err != nil {
		messenger.Error(session.ID, err)
		return err
	}
	session = result.Session
	app.Logger.Info("agent.run", "session", session.ID, "iterations", result.Iterations, "tool_calls", result.ToolCalls)
	return nil
}

// loadOrCreateSession resuelve la sesión activa.
func loadOrCreateSession(ctx context.Context, app *apppkg.App, sessionID, workspace string,
	model domain.Model, agent string) (domain.Session, error) {
	if sessionID != "" {
		return app.SessionService.Resume(ctx, sessionID)
	}
	return app.SessionService.Create(ctx, workspace, model, agent)
}

func runInteractive(app *apppkg.App, cmd *cobra.Command) error {
	return tui.Run(app)
}
