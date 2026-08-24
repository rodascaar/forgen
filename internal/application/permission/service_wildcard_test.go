package permission

import (
	"context"
	"testing"

	"github.com/rodascaar/forgen/internal/core/domain"
)

func TestWildcard_MCP(t *testing.T) {
	svc := NewService(domain.PermissionModeAuto, "", []domain.PermissionRule{
		{Tool: "mcp_*", Level: domain.PermissionOnRequest},
	}, nil)

	// mcp_filesystem_read debe matchear mcp_*
	dec, _ := svc.Decide(context.Background(), "s1", domain.ToolCall{Name: "mcp_filesystem_read"})
	if dec.Allowed || dec.Level != domain.PermissionOnRequest {
		t.Fatalf("expected wildcard on_request, got %+v", dec)
	}
	// bash no debe matchear
	dec, _ = svc.Decide(context.Background(), "s1", domain.ToolCall{Name: "bash", Arguments: map[string]any{"command": "echo hi"}})
	if !dec.Allowed {
		t.Fatalf("bash should be allowed in auto without rule, got %+v", dec)
	}
}

func TestSubAgent_ToolFiltering(t *testing.T) {
	// Simula explorer que solo permite read/glob/grep : write debe ser negado por AllowedTools del Agent,
	// pero PermissionService en modo auto permite write si no hay regla; el filtrado real ocurre en Runner.visibleTools.
	// Aquí verificamos que una regla wildcard "mcp_*" no afecta a write.
	svc := NewService(domain.PermissionModeAuto, "", []domain.PermissionRule{
		{Tool: "write", Level: domain.PermissionNever},
	}, nil)
	dec, _ := svc.Decide(context.Background(), "s1", domain.ToolCall{Name: "write"})
	if dec.Allowed {
		t.Fatalf("write should be denied by explicit never rule")
	}
	dec, _ = svc.Decide(context.Background(), "s1", domain.ToolCall{Name: "read"})
	if !dec.Allowed {
		t.Fatalf("read should be allowed")
	}
}
