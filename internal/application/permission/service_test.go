package permission_test

import (
	"context"
	"testing"

	"github.com/forgen/forgen/internal/application/permission"
	"github.com/forgen/forgen/internal/core/domain"
)

func TestAutoModeAllowsSafeTools(t *testing.T) {
	service := permission.NewService(domain.PermissionModeAuto, "/ws", nil, nil)
	decision, err := service.Decide(context.Background(), "s1", domain.ToolCall{Name: "read", Arguments: map[string]any{"path": "a.go"}})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if !decision.Allowed {
		t.Fatal("read debería estar permitida en modo auto")
	}
}

func TestNeverModeDeniesEverything(t *testing.T) {
	service := permission.NewService(domain.PermissionModeNever, "/ws", nil, nil)
	decision, err := service.Decide(context.Background(), "s1", domain.ToolCall{Name: "read", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if decision.Allowed {
		t.Fatal("modo never debería denegar incluso read")
	}
}

func TestOnRequestPromptsForSensitive(t *testing.T) {
	service := permission.NewService(domain.PermissionModeOnRequest, "/ws", nil, nil)

	decision, _ := service.Decide(context.Background(), "s1", domain.ToolCall{Name: "bash", Arguments: map[string]any{"command": "ls"}})
	if decision.Allowed || decision.Level != domain.PermissionOnRequest {
		t.Fatalf("bash debería requerir confirmación en on_request, got %+v", decision)
	}

	decision, _ = service.Decide(context.Background(), "s1", domain.ToolCall{Name: "read", Arguments: map[string]any{}})
	if !decision.Allowed {
		t.Fatal("read debería estar permitida en on_request")
	}
}

func TestDangerousCommandAlwaysPrompts(t *testing.T) {
	service := permission.NewService(domain.PermissionModeAuto, "/ws", nil, nil)
	decision, err := service.Decide(context.Background(), "s1", domain.ToolCall{
		Name:      "bash",
		Arguments: map[string]any{"command": "sudo rm -rf /"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if decision.Allowed {
		t.Fatal("comando destructivo no debería ejecutarse automáticamente")
	}
}

func TestExplicitRuleWins(t *testing.T) {
	rules := []domain.PermissionRule{
		{Tool: "bash", Arguments: map[string]any{"command": "go test ./..."}, Level: domain.PermissionAuto, IsExact: true},
	}
	service := permission.NewService(domain.PermissionModeOnRequest, "/ws", rules, nil)
	decision, err := service.Decide(context.Background(), "s1", domain.ToolCall{
		Name:      "bash",
		Arguments: map[string]any{"command": "go test ./..."},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if !decision.Allowed {
		t.Fatal("la regla explícita debería permitir el comando en on_request")
	}
}
