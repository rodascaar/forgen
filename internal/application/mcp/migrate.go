package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/rodascaar/forgen/internal/core/domain"
)

// MigrateFromWellKnownPaths busca MCP servers en Claude Code, OpenCode y Cursor
// y devuelve un mapa nombre→config (existing wins se resuelve en el caller).
func MigrateFromWellKnownPaths() (map[string]domain.MCPServerConfig, error) {
	home, _ := os.UserHomeDir()
	result := make(map[string]domain.MCPServerConfig)

	// 1. Claude Code: ~/.claude.json { mcpServers: {name: {command, args, env}}, projects: {path: {mcpServers}} }
	_ = migrateClaudeCode(filepath.Join(home, ".claude.json"), result)
	// 2. OpenCode: varios paths (ver kimchi agent-discovery)
	for _, p := range []string{
		filepath.Join(home, ".config", "opencode", "opencode.json"),
		filepath.Join(home, ".config", "opencode", "opencode.jsonc"),
		"opencode.json",
		"opencode.jsonc",
	} {
		_ = migrateOpenCode(p, result)
	}
	if cfg := os.Getenv("OPENCODE_CONFIG"); cfg != "" {
		_ = migrateOpenCode(cfg, result)
	}
	// 3. Cursor: ~/.cursor/mcp.json, ~/.config/cursor/mcp.json
	for _, p := range []string{
		filepath.Join(home, ".cursor", "mcp.json"),
		filepath.Join(home, ".config", "cursor", "mcp.json"),
	} {
		_ = migrateCursor(p, result)
	}
	return result, nil
}

func migrateClaudeCode(path string, out map[string]domain.MCPServerConfig) error {
	data, err := os.ReadFile(filepath.Clean(path)) //nolint:gosec // path proviene de constantes/env controlados
	if err != nil {
		return err
	}
	var raw struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
		Projects map[string]struct {
			MCPServers map[string]struct {
				Command string            `json:"command"`
				Args    []string          `json:"args"`
				Env     map[string]string `json:"env"`
			} `json:"mcpServers"`
		} `json:"projects"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for name, srv := range raw.MCPServers {
		if _, ok := out[name]; !ok {
			out[name] = domain.MCPServerConfig{Type: "stdio", Command: srv.Command, Args: srv.Args, Env: srv.Env}
		}
	}
	for _, proj := range raw.Projects {
		for name, srv := range proj.MCPServers {
			if _, ok := out[name]; !ok {
				out[name] = domain.MCPServerConfig{Type: "stdio", Command: srv.Command, Args: srv.Args, Env: srv.Env}
			}
		}
	}
	return nil
}

func migrateOpenCode(path string, out map[string]domain.MCPServerConfig) error {
	data, err := os.ReadFile(filepath.Clean(path)) //nolint:gosec // path proviene de constantes/env controlados
	if err != nil {
		return err
	}
	var raw struct {
		MCP        map[string]domain.MCPServerConfig `json:"mcp"`
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
			Type    string            `json:"type"`
			Enabled *bool             `json:"enabled"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for name, srv := range raw.MCP {
		if _, ok := out[name]; !ok {
			out[name] = srv
		}
	}
	for name, srv := range raw.MCPServers {
		if srv.Enabled != nil && !*srv.Enabled {
			continue
		}
		if _, ok := out[name]; !ok {
			cfg := domain.MCPServerConfig{Type: srv.Type, Command: srv.Command, Args: srv.Args, Env: srv.Env, URL: srv.URL, Headers: srv.Headers}
			if cfg.Type == "" && cfg.URL != "" {
				cfg.Type = "http"
			}
			out[name] = cfg
		}
	}
	return nil
}

func migrateCursor(path string, out map[string]domain.MCPServerConfig) error {
	data, err := os.ReadFile(filepath.Clean(path)) //nolint:gosec // path proviene de constantes/env controlados
	if err != nil {
		return err
	}
	var raw struct {
		MCPServers map[string]struct {
			Command  string            `json:"command"`
			Args     []string          `json:"args"`
			Env      map[string]string `json:"env"`
			URL      string            `json:"url"`
			Headers  map[string]string `json:"headers"`
			Type     string            `json:"type"`
			Disabled bool              `json:"disabled"`
		} `json:"mcpServers"`
		Servers map[string]struct {
			Command  string            `json:"command"`
			Args     []string          `json:"args"`
			Env      map[string]string `json:"env"`
			URL      string            `json:"url"`
			Headers  map[string]string `json:"headers"`
			Type     string            `json:"type"`
			Disabled bool              `json:"disabled"`
		} `json:"servers"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		// Cursor también soporta formato { "mcpServers": {...} } directo
		var alt map[string]struct {
			Command  string            `json:"command"`
			Args     []string          `json:"args"`
			Env      map[string]string `json:"env"`
			URL      string            `json:"url"`
			Headers  map[string]string `json:"headers"`
			Type     string            `json:"type"`
			Disabled bool              `json:"disabled"`
		}
		if err2 := json.Unmarshal(data, &alt); err2 != nil {
			return err
		}
		for name, srv := range alt {
			if srv.Disabled {
				continue
			}
			if _, ok := out[name]; !ok {
				out[name] = domain.MCPServerConfig{Type: srv.Type, Command: srv.Command, Args: srv.Args, Env: srv.Env, URL: srv.URL, Headers: srv.Headers}
			}
		}
		return nil
	}
	for name, srv := range raw.MCPServers {
		if srv.Disabled {
			continue
		}
		if _, ok := out[name]; !ok {
			out[name] = domain.MCPServerConfig{Type: srv.Type, Command: srv.Command, Args: srv.Args, Env: srv.Env, URL: srv.URL, Headers: srv.Headers}
		}
	}
	for name, srv := range raw.Servers {
		if srv.Disabled {
			continue
		}
		if _, ok := out[name]; !ok {
			out[name] = domain.MCPServerConfig{Type: srv.Type, Command: srv.Command, Args: srv.Args, Env: srv.Env, URL: srv.URL, Headers: srv.Headers}
		}
	}
	return nil
}
