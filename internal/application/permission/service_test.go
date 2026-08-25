package permission_test

import (
	"context"
	"testing"

	"github.com/rodascaar/forgen/internal/application/permission"
	"github.com/rodascaar/forgen/internal/core/domain"
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

func TestSensitiveReadPromptsInAutoMode(t *testing.T) {
	service := permission.NewService(domain.PermissionModeAuto, "/ws", nil, nil)
	decision, err := service.Decide(context.Background(), "s1", domain.ToolCall{Name: "read", Arguments: map[string]any{"path": ".env"}})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if decision.Allowed || decision.Level != domain.PermissionOnRequest {
		t.Fatalf("read .env debería pedir confirmación incluso en auto, got %+v", decision)
	}

	decision, _ = service.Decide(context.Background(), "s1", domain.ToolCall{Name: "read", Arguments: map[string]any{"path": "a.go"}})
	if !decision.Allowed {
		t.Fatal("read de archivo normal debería permitirse en auto")
	}
}

func TestSensitiveReadReadManyFiles(t *testing.T) {
	service := permission.NewService(domain.PermissionModeAuto, "/ws", nil, nil)
	decision, _ := service.Decide(context.Background(), "s1", domain.ToolCall{Name: "read_many_files", Arguments: map[string]any{"paths": []any{"a.go", ".env"}}})
	if decision.Allowed {
		t.Fatal("read_many_files con .env debería requerir confirmación")
	}
}

func TestSensitiveReadCredentials(t *testing.T) {
	service := permission.NewService(domain.PermissionModeAuto, "/ws", nil, nil)
	for _, p := range []string{"~/.aws/credentials", "id_rsa", ".ssh/id_ed25519"} {
		decision, _ := service.Decide(context.Background(), "s1", domain.ToolCall{Name: "read", Arguments: map[string]any{"path": p}})
		if decision.Allowed {
			t.Fatalf("read %s debería requerir confirmación", p)
		}
	}
}

func TestOutsideWorkspacePromptsInAuto(t *testing.T) {
	service := permission.NewService(domain.PermissionModeAuto, "/ws", nil, nil)
	cases := []string{"../../etc/passwd", "/etc/passwd", "~/secrets/x", "../x.go"}
	for _, p := range cases {
		decision, _ := service.Decide(context.Background(), "s1", domain.ToolCall{Name: "read", Arguments: map[string]any{"path": p}})
		if decision.Allowed {
			t.Fatalf("read %s fuera del workspace debería requerir confirmación en auto", p)
		}
	}
	// dentro NO pregunta
	decision, _ := service.Decide(context.Background(), "s1", domain.ToolCall{Name: "read", Arguments: map[string]any{"path": "src/a.go"}})
	if !decision.Allowed {
		t.Fatal("read dentro del workspace no debería requerir confirmación")
	}
}

func TestDatabaseWritePromptsInAuto(t *testing.T) {
	service := permission.NewService(domain.PermissionModeAuto, "/ws", nil, nil)
	for _, db := range []string{"app.db", "data.sqlite3", "cache/db.sqlite"} {
		decision, _ := service.Decide(context.Background(), "s1", domain.ToolCall{Name: "write", Arguments: map[string]any{"path": db}})
		if decision.Allowed {
			t.Fatalf("write %s (base de datos) debería requerir confirmación", db)
		}
	}
	// archivo normal dentro NO pregunta
	decision, _ := service.Decide(context.Background(), "s1", domain.ToolCall{Name: "write", Arguments: map[string]any{"path": "a.go"}})
	if !decision.Allowed {
		t.Fatal("write de archivo normal dentro no debería preguntar")
	}
}

func TestDangerousSQLPromptsInAuto(t *testing.T) {
	service := permission.NewService(domain.PermissionModeAuto, "/ws", nil, nil)
	for _, c := range []string{
		"sqlite3 app.db 'DELETE FROM users'",
		"psql -c 'DROP TABLE users'",
		"mysql -e 'TRUNCATE TABLE logs'",
	} {
		decision, _ := service.Decide(context.Background(), "s1", domain.ToolCall{Name: "bash", Arguments: map[string]any{"command": c}})
		if decision.Allowed {
			t.Fatalf("bash %q (SQL destructivo) debería requerir confirmación", c)
		}
	}
	// SELECT / UPDATE con WHERE seguro no pregunta
	decision, _ := service.Decide(context.Background(), "s1", domain.ToolCall{Name: "bash", Arguments: map[string]any{"command": "sqlite3 app.db 'SELECT * FROM users'"}})
	if !decision.Allowed {
		t.Fatal("SELECT no debería requerir confirmación")
	}
}

func TestRmPromptsInAuto(t *testing.T) {
	service := permission.NewService(domain.PermissionModeAuto, "/ws", nil, nil)
	for _, c := range []string{"rm -f /tmp/x", "rm -rf /etc", "rm -r /home/user", "unlink /etc/passwd", "rmdir /etc"} {
		decision, _ := service.Decide(context.Background(), "s1", domain.ToolCall{Name: "bash", Arguments: map[string]any{"command": c}})
		if decision.Allowed {
			t.Fatalf("bash %q (borrado sistema) debería requerir confirmación", c)
		}
	}
	// rm dentro del workspace sin -f no pregunta (no destructivo de sistema)
	decision, _ := service.Decide(context.Background(), "s1", domain.ToolCall{Name: "bash", Arguments: map[string]any{"command": "rm tmp/output.txt"}})
	if !decision.Allowed {
		t.Fatal("rm de archivo local sin flags no debería preguntar")
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
