package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/rodascaar/forgen/internal/adapters/out/storage"
	apppkg "github.com/rodascaar/forgen/internal/app"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/spf13/cobra"
)

func newPermissionsCommand(app *apppkg.App) *cobra.Command {
	cmd := &cobra.Command{Use: "permissions", Short: "Gestiona reglas de permiso persistentes (fx-style)"}
	cmd.AddCommand(newPermissionsListCmd(app))
	cmd.AddCommand(newPermissionsRememberCmd(app))
	cmd.AddCommand(newPermissionsRevokeCmd(app))
	return cmd
}

func newPermissionsListCmd(app *apppkg.App) *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "Lista reglas persistentes",
		RunE: func(cmd *cobra.Command, args []string) error {
			store := storage.NewJSONPermissionStore(app.Paths.RulesFile)
			rules, err := store.Load(cmd.Context())
			if err != nil {
				return err
			}
			if len(rules) == 0 {
				fmt.Println("Sin reglas persistentes.")
				return nil
			}
			for _, r := range rules {
				id := permissionRuleID(r)
				argsJSON, _ := json.Marshal(r.Arguments)
				fmt.Printf("%s  %-12s  %-10s  %s\n", id[:8], r.Tool, r.Level, string(argsJSON))
			}
			return nil
		},
	}
}

func newPermissionsRememberCmd(app *apppkg.App) *cobra.Command {
	return &cobra.Command{
		Use: "remember <allow|deny> <tool> <args-json>", Short: "Guarda regla persistente (exact)", Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			levelStr, tool, argsJSON := args[0], args[1], args[2]
			var level domain.PermissionLevel
			switch levelStr {
			case "allow", "auto":
				level = domain.PermissionAuto
			case "deny", "never":
				level = domain.PermissionNever
			case "ask", "on_request":
				level = domain.PermissionOnRequest
			default:
				return fmt.Errorf("level debe ser allow|deny|ask")
			}
			var toolArgs map[string]any
			if err := json.Unmarshal([]byte(argsJSON), &toolArgs); err != nil {
				return fmt.Errorf("args-json inválido: %w", err)
			}
			store := storage.NewJSONPermissionStore(app.Paths.RulesFile)
			rules, err := store.Load(cmd.Context())
			if err != nil {
				return err
			}
			// workspace actual
			workspace := ""
			if cwd, err := app.FileSystem.Exists(cmd.Context(), "."); err == nil && cwd {
				// use FileSystem workspace is current dir; store absolute
				workspace = "" // global por defecto; si se quiere workspace, pasar via flag futuro
			}
			rule := domain.PermissionRule{Tool: tool, Arguments: toolArgs, Level: level, Workspace: workspace, IsExact: true}
			// Evitar duplicados por ID
			newID := permissionRuleID(rule)
			for _, r := range rules {
				if permissionRuleID(r) == newID {
					fmt.Printf("Regla ya existe: %s\n", newID[:8])
					return nil
				}
			}
			rules = append(rules, rule)
			if err := store.Save(cmd.Context(), rules); err != nil {
				return err
			}
			fmt.Printf("Regla %s guardada: %s %s\n", newID[:8], tool, level)
			return nil
		},
	}
}

func newPermissionsRevokeCmd(app *apppkg.App) *cobra.Command {
	return &cobra.Command{
		Use: "revoke <rule-id>", Short: "Elimina regla por ID (prefijo 8 chars)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prefix := args[0]
			store := storage.NewJSONPermissionStore(app.Paths.RulesFile)
			rules, err := store.Load(cmd.Context())
			if err != nil {
				return err
			}
			kept := make([]domain.PermissionRule, 0, len(rules))
			found := false
			for _, r := range rules {
				id := permissionRuleID(r)
				if len(prefix) <= len(id) && id[:len(prefix)] == prefix {
					found = true
					continue
				}
				kept = append(kept, r)
			}
			if !found {
				return fmt.Errorf("regla %q no encontrada", prefix)
			}
			if err := store.Save(cmd.Context(), kept); err != nil {
				return err
			}
			fmt.Printf("Regla %s revocada\n", prefix)
			return nil
		},
	}
}

func permissionRuleID(r domain.PermissionRule) string {
	argsJSON, _ := json.Marshal(r.Arguments)
	h := sha256.Sum256([]byte(r.Tool + ":" + string(argsJSON) + ":" + r.Workspace))
	return hex.EncodeToString(h[:])
}
