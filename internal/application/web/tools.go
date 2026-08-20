// Package web expone las herramientas web_fetch y web_search.
package web

import (
	"context"
	"fmt"
	"strings"

	"github.com/forgen/forgen/internal/adapters/out/webfetch"
	"github.com/forgen/forgen/internal/application/tools"
	"github.com/forgen/forgen/internal/core/domain"
	"github.com/forgen/forgen/internal/core/ports"
)

// NewWebFetchTool construye la herramienta web_fetch.
func NewWebFetchTool() tools.ToolDef {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{"type": "string", "description": "URL a descargar (http/https)"},
		},
		"required": []string{"url"},
	}
	return tools.ToolDef{
		Tool: domain.Tool{
			Name:        "web_fetch",
			Description: "Descarga una URL y devuelve su contenido como texto plano.",
			Status:      domain.ToolStatusEnabled,
			Schema:      schema,
		},
		Execute: func(ctx context.Context, raw map[string]any) domain.ToolResult {
			url, _ := raw["url"].(string)
			if url == "" {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("web_fetch requiere 'url'")}
			}
			text, err := webfetch.Fetch(ctx, url)
			if err != nil {
				return domain.ToolResult{OK: false, Error: err}
			}
			return domain.ToolResult{OK: true, Output: text}
		},
	}
}

// NewWebSearchTool construye la herramienta web_search con un provider opcional.
func NewWebSearchTool(provider ports.SearchProvider) tools.ToolDef {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Consulta de búsqueda"},
			"limit": map[string]any{"type": "integer", "description": "Número de resultados (máx 10)"},
		},
		"required": []string{"query"},
	}
	return tools.ToolDef{
		Tool: domain.Tool{
			Name:        "web_search",
			Description: "Busca en la web y devuelve resultados con título, URL y resumen.",
			Status:      domain.ToolStatusEnabled,
			Schema:      schema,
		},
		Execute: func(ctx context.Context, raw map[string]any) domain.ToolResult {
			query, _ := raw["query"].(string)
			if query == "" {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("web_search requiere 'query'")}
			}
			if provider == nil {
				return domain.ToolResult{OK: false, Error: fmt.Errorf(
					"búsqueda web no configurada: define 'search.provider' y 'search.api_key_env' en la config")}
			}
			limit := 5
			if value, ok := raw["limit"].(float64); ok && value > 0 {
				limit = int(value)
			}
			results, err := provider.Search(ctx, query, limit)
			if err != nil {
				return domain.ToolResult{OK: false, Error: err}
			}
			if len(results) == 0 {
				return domain.ToolResult{OK: true, Output: "(sin resultados)"}
			}
			var builder strings.Builder
			for _, result := range results {
				fmt.Fprintf(&builder, "• %s\n  %s\n  %s\n", result.Title, result.URL, result.Snippet)
			}
			return domain.ToolResult{OK: true, Output: strings.TrimSpace(builder.String())}
		},
	}
}
