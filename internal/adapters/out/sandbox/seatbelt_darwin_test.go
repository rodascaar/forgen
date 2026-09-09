//go:build darwin

package sandbox

import (
	"context"
	"strings"
	"testing"

	"github.com/rodascaar/forgen/internal/core/domain"
)

func TestBuildSeatbeltProfileOrder(t *testing.T) {
	policy := domain.SandboxPolicy{
		Mode:          domain.SandboxWorkspaceWrite,
		Workspace:     "/repo",
		ReadOnlyPaths: []string{"/repo/.git"},
	}
	profile := BuildSeatbeltProfile(policy)
	allowIdx := strings.Index(profile, "(allow file-write* (regex")
	denyIdx := strings.Index(profile, "(deny file-write* (regex")
	if allowIdx < 0 || denyIdx < 0 {
		t.Fatalf("perfil debe tener allow y deny de escritura:\n%s", profile)
	}
	if denyIdx < allowIdx {
		t.Fatalf("deny .git debe ir DESPUÉS del allow (last-match wins):\n%s", profile)
	}
	if strings.Contains(profile, "network") {
		t.Fatalf("sin red no debe haber regla network:\n%s", profile)
	}
	// Sin doble-escape: `\\.` no casa `.git` (bug real encontrado en validación).
	if strings.Contains(profile, `\\.git`) {
		t.Fatalf("regex con doble backslash no casa .git:\n%s", profile)
	}
	if !strings.Contains(profile, `\.git`) {
		t.Fatalf("deny debe escapar el punto de .git:\n%s", profile)
	}
}

func TestBuildSeatbeltProfileNetworkAndReadOnly(t *testing.T) {
	policy := domain.SandboxPolicy{Mode: domain.SandboxWorkspaceWrite, Workspace: "/repo", AllowNetwork: true}
	if !strings.Contains(BuildSeatbeltProfile(policy), "(allow network*)") {
		t.Fatalf("con red debe haber (allow network*)")
	}
	ro := domain.SandboxPolicy{Mode: domain.SandboxReadOnly, Workspace: "/repo"}
	if strings.Contains(BuildSeatbeltProfile(ro), "(allow file-write*)") {
		t.Fatalf("read-only no debe permitir escritura")
	}
}

func TestSeatbeltAvailable(t *testing.T) {
	if err := (SeatbeltBackend{}).Available(context.Background()); err != nil {
		t.Skipf("sandbox-exec no disponible aquí: %v", err)
	}
}

func TestSeatbeltEnforcesGitDeny(t *testing.T) {
	backend := SeatbeltBackend{}
	if err := backend.Available(context.Background()); err != nil {
		t.Skipf("sin sandbox-exec: %v", err)
	}
	dir := t.TempDir()
	policy := domain.SandboxPolicy{
		Mode:          domain.SandboxWorkspaceWrite,
		Workspace:     dir,
		ReadOnlyPaths: []string{dir + "/.git"},
	}
	ctx := context.Background()
	if _, err := backend.Execute(ctx, policy, "mkdir -p .git && echo ok > hello.txt", dir, nil); err != nil {
		t.Fatalf("escritura en workspace debe pasar: %v", err)
	}
	res, _ := backend.Execute(ctx, policy, "echo evil > .git/evil.txt", dir, nil)
	if res.ExitCode == 0 {
		t.Fatalf(".git debe denegar escritura (exit!=0)")
	}
}
