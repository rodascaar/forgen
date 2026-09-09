package sandbox

import (
	"strings"
	"testing"

	"github.com/rodascaar/forgen/internal/core/domain"
)

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
	policy := domain.SandboxPolicy{
		Mode:          domain.SandboxWorkspaceWrite,
		Workspace:     "/repo",
		ReadOnlyPaths: []string{"/repo/.git"},
	}
	args := buildBwrapArgs(policy, "go test ./...", "/repo")
	joined := strings.Join(args, " ")
	for _, want := range []string{"--ro-bind / /", "--bind /repo /repo", "--ro-bind /repo/.git /repo/.git", "--unshare-net", "--chdir /repo", "go test ./..."} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args %q debe contener %q", joined, want)
		}
	}
	// Con red permitida no hay --unshare-net.
	policy.AllowNetwork = true
	if strings.Contains(strings.Join(buildBwrapArgs(policy, "x", "/repo"), " "), "--unshare-net") {
		t.Fatalf("con red permitida no debe haber --unshare-net")
	}
	// read-only no bindea workspace escribible.
	policy.Mode = domain.SandboxReadOnly
	policy.AllowNetwork = false
	joined = strings.Join(buildBwrapArgs(policy, "x", "/repo"), " ")
	if strings.Contains(joined, "--bind /repo /repo") {
		t.Fatalf("read-only no debe bindear workspace rw: %q", joined)
	}
}

func TestSandboxPromptBlock(t *testing.T) {
	policy := domain.DefaultSandboxPolicy("/repo")
	block := policy.PromptBlock()
	for _, want := range []string{"workspace-write", "/repo", ".git", "network=deny"} {
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
