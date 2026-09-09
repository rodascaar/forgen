package sandbox

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rodascaar/forgen/internal/core/domain"
)

// testWorkspace devuelve un workspace portable: en Windows filepath lo
// convierte a `\repo`, igual que hace el código productivo con Clean.
// Los tests nunca deben asumir separadores Unix (rompió CI windows).
func testWorkspace() string { return filepath.FromSlash("/repo") }

func TestSelectBackend(t *testing.T) {
	if b := selectBackend("linux"); b == nil || b.Name() != "bwrap" {
		t.Fatalf("linux debe usar bwrap, got %v", b)
	}
	if b := selectBackend("windows"); b != nil {
		t.Fatalf("windows debe degradar (nil), got %v", b.Name())
	}
	if b := selectBackend("plan9"); b != nil {
		t.Fatalf("SO desconocido debe degradar (nil), got %v", b.Name())
	}
}

func TestBuildBwrapArgs(t *testing.T) {
	ws := testWorkspace()
	git := filepath.Join(ws, ".git")
	policy := domain.SandboxPolicy{
		Mode:          domain.SandboxWorkspaceWrite,
		Workspace:     ws,
		ReadOnlyPaths: []string{git},
	}
	args := buildBwrapArgs(policy, "go test ./...", ws)
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--ro-bind / /",
		"--bind " + ws + " " + ws,
		"--ro-bind " + git + " " + git,
		"--unshare-net",
		"--chdir " + ws,
		"go test ./...",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q debe contener %q", joined, want)
		}
	}
	// Con red permitida no hay --unshare-net.
	policy.AllowNetwork = true
	if strings.Contains(strings.Join(buildBwrapArgs(policy, "x", ws), " "), "--unshare-net") {
		t.Fatalf("con red permitida no debe haber --unshare-net")
	}
	// read-only no bindea workspace escribible.
	policy.Mode = domain.SandboxReadOnly
	policy.AllowNetwork = false
	joined = strings.Join(buildBwrapArgs(policy, "x", ws), " ")
	if strings.Contains(joined, "--bind "+ws+" "+ws) {
		t.Fatalf("read-only no debe bindear workspace rw: %q", joined)
	}
}

func TestSandboxPromptBlock(t *testing.T) {
	ws := testWorkspace()
	policy := domain.DefaultSandboxPolicy(ws)
	block := policy.PromptBlock()
	// Se afirma sobre la forma nativa del SO (policy.Workspace), no "/repo" literal.
	for _, want := range []string{"workspace-write", policy.Workspace, ".git", "network=deny"} {
		if !strings.Contains(block, want) {
			t.Fatalf("PromptBlock %q debe contener %q", block, want)
		}
	}
	if got := domain.ParseSandboxMode("read-only"); got != domain.SandboxReadOnly {
		t.Fatalf("parse read-only = %q", got)
	}
	if got := domain.ParseSandboxMode("peligroso"); got != domain.SandboxWorkspaceWrite {
		t.Fatalf("modo desconocido debe caer a workspace-write, got %q", got)
	}
}

func TestNativeExecutorDegradesGracefully(t *testing.T) {
	native := NewNativeExecutor("/repo", domain.SandboxWorkspaceWrite, false, nil, newNilFallback(), nil)
	if native == nil {
		t.Fatalf("no debe retornar nil")
	}
	// En darwin con sandbox-exec debe estar enforced; en otros degrada sin fallar.
	t.Logf("policy: %+v", native.Policy())
}
