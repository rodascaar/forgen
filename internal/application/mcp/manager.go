// Package mcp implementa el manager que conecta servidores MCP y expone sus
// herramientas al agente.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	mcpadapter "github.com/rodascaar/forgen/internal/adapters/out/mcp"
	"github.com/rodascaar/forgen/internal/application/tools"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// Manager arranca servidores MCP y registra sus tools en el Registry.
type Manager struct {
	registry *tools.Registry
	logger   *slog.Logger
	clients  []ports.MCPClient
}

// NewManager construye el manager de MCP.
func NewManager(registry *tools.Registry, logger *slog.Logger) *Manager {
	return &Manager{registry: registry, logger: logger}
}

// Start arranca los servidores configurados y registra sus herramientas.
// Devuelve un error combinado con los errores de los servidores que no arrancaron
// (no fatal: un servidor caído no rompe al agente).
func (m *Manager) Start(ctx context.Context, servers map[string]domain.MCPServerConfig) error {
	var failures []error
	for name, config := range servers {
		if err := m.startServer(ctx, name, config); err != nil {
			m.logger.Warn("mcp.server.start_failed", "server", name, "err", err)
			failures = append(failures, fmt.Errorf("mcp %s: %w", name, err))
		}
	}
	return errors.Join(failures...)
}

func (m *Manager) startServer(ctx context.Context, name string, config domain.MCPServerConfig) error {
	var client ports.MCPClient
	var err error
	switch config.MCPServerType() {
	case "http", "sse":
		if config.URL == "" {
			return fmt.Errorf("mcp %s: url requerida para type http/sse", name)
		}
		client, err = mcpadapter.NewHTTPClient(config.URL, config.Headers)
	default: // stdio
		client, err = mcpadapter.NewStdioClient(config.Command, config.Args, config.Env)
	}
	if err != nil {
		return err
	}
	mcpTools, err := client.ListTools(ctx)
	if err != nil {
		_ = client.Close()
		return err
	}
	m.clients = append(m.clients, client)

	for _, mcpTool := range mcpTools {
		toolName := name + "_" + mcpTool.Name
		m.registry.Register(newMCPToolDef(toolName, mcpTool, client))
		m.logger.Info("mcp.tool.registered", "server", name, "tool", toolName)
	}
	return nil
}

// Close cierra todos los clientes MCP.
func (m *Manager) Close() {
	for _, client := range m.clients {
		_ = client.Close()
	}
}

// newMCPToolDef convierte una tool MCP en una tool del registry.
func newMCPToolDef(name string, mcpTool ports.MCPTool, client ports.MCPClient) tools.ToolDef {
	schema := mcpTool.InputSchema
	if schema == nil {
		schema = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return tools.ToolDef{
		Tool: domain.Tool{
			Name:        name,
			Description: mcpTool.Description,
			Status:      domain.ToolStatusEnabled,
			Schema:      schema,
		},
		Execute: func(ctx context.Context, raw map[string]any) domain.ToolResult {
			output, err := client.CallTool(ctx, mcpTool.Name, raw)
			if err != nil {
				return domain.ToolResult{OK: false, Error: err}
			}
			return domain.ToolResult{OK: true, Output: output}
		},
	}
}
