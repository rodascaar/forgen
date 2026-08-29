package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	apppkg "github.com/rodascaar/forgen/internal/app"
	"github.com/rodascaar/forgen/internal/application/mcp"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/spf13/cobra"
)

func newMCPCommand(app *apppkg.App) *cobra.Command {
	cmd := &cobra.Command{Use: "mcp", Short: "Gestiona servidores MCP (Model Context Protocol)"}
	cmd.AddCommand(newMCPListCmd(app))
	cmd.AddCommand(newMCPAddCmd(app))
	cmd.AddCommand(newMCPRemoveCmd(app))
	cmd.AddCommand(newMCPTestCmd(app))
	cmd.AddCommand(newMCPMigrateCmd(app))
	return cmd
}

func newMCPListCmd(app *apppkg.App) *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "Lista servidores MCP configurados",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := app.ConfigService.Load(cmd.Context())
			if err != nil {
				return err
			}
			if len(cfg.MCPServers) == 0 {
				fmt.Println("Sin servidores MCP configurados.")
				return nil
			}
			for name, s := range cfg.MCPServers {
				typ := s.MCPServerType()
				target := s.Command
				if s.URL != "" {
					target = s.URL
				}
				fmt.Printf("%s  %-8s  %s\n", name, typ, target)
			}
			return nil
		},
	}
}

func newMCPAddCmd(app *apppkg.App) *cobra.Command {
	var mcpType, command, url string
	var headers []string
	cmd := &cobra.Command{
		Use: "add <nombre>", Short: "Añade un servidor MCP", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if mcpType == "" {
				if url != "" {
					mcpType = "http"
				} else {
					mcpType = "stdio"
				}
			}
			cfg, err := app.ConfigService.Load(cmd.Context())
			if err != nil {
				return err
			}
			if cfg.MCPServers == nil {
				cfg.MCPServers = make(map[string]domain.MCPServerConfig)
			}
			mcpCfg := domain.MCPServerConfig{Type: mcpType, Command: command, URL: url}
			if len(headers) > 0 {
				mcpCfg.Headers = make(map[string]string)
				for _, h := range headers {
					var k, v string
					for i, c := range h {
						if c == '=' || c == ':' {
							k = h[:i]
							v = h[i+1:]
							break
						}
					}
					if k != "" {
						mcpCfg.Headers[k] = v
					}
				}
			}
			if mcpCfg.MCPServerType() == "stdio" && mcpCfg.Command == "" {
				return fmt.Errorf("para type stdio se requiere --command")
			}
			if (mcpCfg.MCPServerType() == "http" || mcpCfg.MCPServerType() == "sse") && mcpCfg.URL == "" {
				return fmt.Errorf("para type http/sse se requiere --url")
			}
			cfg.MCPServers[name] = mcpCfg
			if err := cfg.Validate(); err != nil {
				return err
			}
			if err := app.ConfigService.Save(cmd.Context(), cfg); err != nil {
				return err
			}
			fmt.Printf("MCP %s añadido (%s)\n", name, mcpCfg.MCPServerType())
			return nil
		},
	}
	cmd.Flags().StringVar(&mcpType, "type", "", "stdio|http|sse")
	cmd.Flags().StringVar(&command, "command", "", "comando para stdio (ej: npx)")
	cmd.Flags().StringVar(&url, "url", "", "url para http/sse")
	cmd.Flags().StringArrayVar(&headers, "header", nil, "headers http (k=v) repetible")
	return cmd
}

func newMCPRemoveCmd(app *apppkg.App) *cobra.Command {
	return &cobra.Command{
		Use: "remove <nombre>", Short: "Elimina un servidor MCP", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := app.ConfigService.Load(cmd.Context())
			if err != nil {
				return err
			}
			if _, ok := cfg.MCPServers[name]; !ok {
				return fmt.Errorf("mcp %q no existe", name)
			}
			delete(cfg.MCPServers, name)
			return app.ConfigService.Save(cmd.Context(), cfg)
		},
	}
}

func newMCPTestCmd(app *apppkg.App) *cobra.Command {
	return &cobra.Command{
		Use: "test <nombre>", Short: "Prueba conexión a un servidor MCP y lista tools", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := app.ConfigService.Load(cmd.Context())
			if err != nil {
				return err
			}
			mcpCfg, ok := cfg.MCPServers[name]
			if !ok {
				return fmt.Errorf("mcp %q no existe", name)
			}
			mgr := mcp.NewManager(app.ToolRegistry, app.Logger)
			if err := mgr.Start(cmd.Context(), map[string]domain.MCPServerConfig{name: mcpCfg}); err != nil {
				return err
			}
			defer mgr.Close()
			fmt.Printf("MCP %s OK — tools registradas\n", name)
			for _, t := range app.ToolRegistry.ListTools() {
				// filtrar por prefijo del server (heurística)
				if len(t.Name) > len(name) && t.Name[:len(name)] == name {
					fmt.Printf("  - %s: %s\n", t.Name, t.Description)
				}
			}
			return nil
		},
	}
}

func newMCPMigrateCmd(app *apppkg.App) *cobra.Command {
	return &cobra.Command{
		Use: "migrate", Short: "Migra MCP servers desde Claude Code / OpenCode / Cursor",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCPMigrate(cmd.Context(), app)
		},
	}
}

func runMCPMigrate(ctx context.Context, app *apppkg.App) error {
	migrated, err := mcp.MigrateFromWellKnownPaths()
	if err != nil {
		return err
	}
	if len(migrated) == 0 {
		fmt.Println("No se encontraron MCP servers para migrar (Claude Code, OpenCode, Cursor).")
		return nil
	}
	cfg, err := app.ConfigService.Load(ctx)
	if err != nil {
		return err
	}
	if cfg.MCPServers == nil {
		cfg.MCPServers = make(map[string]domain.MCPServerConfig)
	}
	added := 0
	for name, srv := range migrated {
		if _, exists := cfg.MCPServers[name]; exists {
			fmt.Printf("  skip %s (ya existe)\n", name)
			continue
		}
		cfg.MCPServers[name] = srv
		added++
		fmt.Printf("  + %s (%s) desde migración\n", name, srv.MCPServerType())
	}
	if added == 0 {
		fmt.Println("Nada que migrar.")
		return nil
	}
	if err := app.ConfigService.Save(ctx, cfg); err != nil {
		return err
	}
	fmt.Printf("Migrados %d servidores.\n", added)
	// Mostrar resumen JSON opcional si FORGEN_MCP_MIGRATE_JSON
	if os.Getenv("FORGEN_MCP_MIGRATE_JSON") != "" {
		data, _ := json.MarshalIndent(migrated, "", "  ")
		fmt.Println(string(data))
	}
	return nil
}
