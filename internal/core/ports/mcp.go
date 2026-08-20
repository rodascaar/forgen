package ports

import "context"

// MCPTool describe una herramienta expuesta por un servidor MCP.
type MCPTool struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// MCPClient es el puerto hacia un servidor MCP (Model Context Protocol).
type MCPClient interface {
	// ListTools devuelve las herramientas expuestas por el servidor.
	ListTools(ctx context.Context) ([]MCPTool, error)
	// CallTool invoca una herramienta y devuelve su texto resultado.
	CallTool(ctx context.Context, name string, arguments map[string]any) (string, error)
	// Close cierra la conexión con el servidor.
	Close() error
}
