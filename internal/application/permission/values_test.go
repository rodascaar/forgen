package permission_test

import (
	"context"
	"testing"

	"github.com/rodascaar/forgen/internal/application/permission"
	"github.com/rodascaar/forgen/internal/core/domain"
)

// TestDecideRespectsSubsetRule verifica que una regla de subconjunto (remember)
// coincide aunque la llamada traiga más argumentos.
func TestDecideRespectsSubsetRule(t *testing.T) {
	svc := permission.NewService(domain.PermissionMode("on_request"), "/ws", []domain.PermissionRule{
		{Tool: "bash", Arguments: map[string]any{"command": "go test ./..."}, Level: domain.PermissionAuto},
	}, nil)
	decision, err := svc.Decide(context.Background(), "s", domain.ToolCall{
		Name:      "bash",
		Arguments: map[string]any{"command": "go test ./...", "workdir": "/ws"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("regla subset auto debe permitir, got %+v", decision)
	}
}

// TestDecideRuleNumericCoercion: regla con int vs llamada con float64 del JSON.
func TestDecideRuleNumericCoercion(t *testing.T) {
	svc := permission.NewService(domain.PermissionMode("on_request"), "/ws", []domain.PermissionRule{
		{Tool: "task", Arguments: map[string]any{"timeout": 60}, Level: domain.PermissionAuto},
	}, nil)
	decision, err := svc.Decide(context.Background(), "s", domain.ToolCall{
		Name:      "task",
		Arguments: map[string]any{"timeout": 60.0, "description": "x", "prompt": "y"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("coerción numérica int/float64 debe coincidir, got %+v", decision)
	}
}
