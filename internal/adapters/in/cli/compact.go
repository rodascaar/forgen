package cli

import (
	"fmt"
	"os"
	"strings"

	apppkg "github.com/rodascaar/forgen/internal/app"
	"github.com/spf13/cobra"
)

func newCompactCommand(app *apppkg.App) *cobra.Command {
	var sessionID string
	var focus string
	cmd := &cobra.Command{
		Use:   "compact [focus]",
		Short: "Compacta el historial de una sesión (prune + LLM summary)",
		Long: `Aplica compaction 2-step: prune no-destructivo (cero LLM) + LLM summary 5 headings.
Preserva últimos 40k tokens, protege read_skill y 2 turnos usuario.
Soporta /compact [focus] para guiar el resumen (Claude Compact Instructions).
Agnóstico a idioma: respeta es/en de la sesión.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && focus == "" {
				focus = strings.Join(args, " ")
			}
			if sessionID == "" {
				return fmt.Errorf("requiere --session <id>")
			}
			ctx := cmd.Context()
			sess, err := app.SessionService.Resume(ctx, sessionID)
			if err != nil {
				return err
			}
			workspace, _ := os.Getwd()
			if sess.Workspace != "" {
				workspace = sess.Workspace
			}
			appConfig, err := app.LoadConfig(ctx)
			if err != nil {
				return err
			}
			model, provider, _, err := app.ResolveRunModel(ctx, sess.Summary(), "", "")
			if err != nil {
				model = sess.Model
				if _, ok := appConfig.FindProvider(model.Provider); ok {
					provider, _ = app.ResolveProvider(appConfig, model)
				}
			}
			if provider == nil {
				return fmt.Errorf("no hay provider disponible para compactar")
			}
			agentDef, _ := app.SelectedAgent(appConfig, sess.Agent)
			messenger := newTextMessenger(os.Stdout, os.Stdin)
			runner, err := app.NewRunner(ctx, apppkg.RunnerDeps{
				Provider:  provider,
				Model:     model,
				Agent:     agentDef,
				Messenger: messenger,
				Responder: messenger,
				Workspace: workspace,
				SessionID: sess.ID,
			})
			if err != nil {
				return err
			}
			if err := runner.CompactNow(ctx, &sess, focus); err != nil {
				return err
			}
			fmt.Printf("Sesión %s compactada: boundary=%d summary=%d chars compactions=%d\n", sess.ID, sess.CompactBoundary, len(sess.CompactionSummary), sess.CompactionCount)
			if sess.CompactionCount >= 3 {
				fmt.Println("Aviso: 3 compactaciones seguidas — considera iniciar sesión fresca (ver /context)")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", "ID de sesión a compactar")
	cmd.Flags().StringVar(&focus, "focus", "", "Instrucciones de enfoque para el resumen (ej: 'focus on API changes')")
	_ = cmd.MarkFlagRequired("session")
	return cmd
}

func newContextCommand(app *apppkg.App) *cobra.Command {
	var sessionID string
	cmd := &cobra.Command{
		Use:   "context",
		Short: "Muestra uso de contexto y compactaciones (como Claude /context)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if sessionID == "" && len(args) > 0 {
				sessionID = args[0]
			}
			if sessionID == "" {
				return fmt.Errorf("requiere --session <id> o arg <id>")
			}
			sess, err := app.SessionService.Resume(cmd.Context(), sessionID)
			if err != nil {
				return err
			}
			appConfig, err := app.LoadConfig(cmd.Context())
			if err != nil {
				appConfig, _ = app.LoadConfig(cmd.Context())
			}
			// Estimar tokens
			tokens := 0
			for _, m := range sess.Messages {
				tokens += len(m.Text())/4 + 4
			}
			limit := 128000
			if md, ok := appConfig.ModelMetadata[sess.Model.Key()]; ok && md.ContextLimit > 0 {
				limit = md.ContextLimit
			}
			pct := float64(tokens) / float64(limit) * 100
			fmt.Printf("Sesión %s\n  Modelo: %s\n  Mensajes: %d\n  Tokens estimados: %d / %d (%.1f%%)\n  Compactions: %d  Boundary: %d\n  Summary: %d chars\n",
				sess.ID, sess.Model.Key(), len(sess.Messages), tokens, limit, pct, sess.CompactionCount, sess.CompactBoundary, len(sess.CompactionSummary))
			if sess.CompactionCount >= 3 {
				fmt.Println("  ⚠  3+ compactions — considera forgen sessions new")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&sessionID, "session", "", "ID de sesión")
	return cmd
}
