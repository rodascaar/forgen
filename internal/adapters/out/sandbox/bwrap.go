// Package sandbox — backend bubblewrap (Linux).
//
// Usa bwrap del sistema si está en PATH (igual que Codex: prefiere el del
// sistema). Sin bwrap, Available() falla y el manager degrada con warning.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// BwrapBackend impone SandboxPolicy vía bubblewrap (Linux).
type BwrapBackend struct{}

func (BwrapBackend) Name() string { return "bwrap" }

// Available verifica bwrap en PATH (fuera del cwd, como Codex).
func (BwrapBackend) Available(ctx context.Context) error {
	path, err := exec.LookPath("bwrap")
	if err != nil {
		return fmt.Errorf("bwrap: no está en PATH (instálalo con tu package manager): %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, path, "--version").Run(); err != nil {
		return fmt.Errorf("bwrap: prueba falló: %w", err)
	}
	return nil
}

// Execute corre el comando confinado: raíz read-only + binds de escritura.
func (BwrapBackend) Execute(ctx context.Context, policy domain.SandboxPolicy, command, workdir string, env []string) (ports.ExecResult, error) {
	if strings.TrimSpace(command) == "" {
		return ports.ExecResult{}, fmt.Errorf("bwrap: comando vacío")
	}
	ws := filepath.Clean(policy.Workspace)
	if workdir == "" {
		workdir = ws
	}
	if !pathInsideWS(ws, workdir) {
		return ports.ExecResult{}, fmt.Errorf("bwrap: workdir %q fuera del workspace %q", workdir, ws)
	}
	args := buildBwrapArgs(policy, command, workdir)
	cmd := exec.CommandContext(ctx, "bwrap", args...)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr limitedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := ports.ExecResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: bwrapExitCode(err)}
	if err != nil && result.ExitCode == 0 {
		return result, fmt.Errorf("bwrap: no se pudo ejecutar: %w", err)
	}
	return result, nil
}

// buildBwrapArgs construye la invocación (pura, testeable sin bwrap instalado).
func buildBwrapArgs(policy domain.SandboxPolicy, command, workdir string) []string {
	ws := filepath.Clean(policy.Workspace)
	args := []string{
		"--die-with-parent",
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--tmpfs", "/tmp",
		"--proc", "/proc",
	}
	if policy.Mode == domain.SandboxFullAccess {
		args = append(args, "--bind", ws, ws)
	} else if policy.Mode != domain.SandboxReadOnly {
		args = append(args, "--bind", ws, ws)
		for _, extra := range policy.ExtraWritable {
			if e := strings.TrimSpace(extra); e != "" {
				args = append(args, "--bind", filepath.Clean(e), filepath.Clean(e))
			}
		}
	}
	for _, ro := range policy.ReadOnlyPaths {
		args = append(args, "--ro-bind", filepath.Clean(ro), filepath.Clean(ro))
	}
	if !policy.AllowNetwork {
		args = append(args, "--unshare-net")
	}
	args = append(args, "--chdir", workdir, "/bin/sh", "-c", command)
	return args
}

func pathInsideWS(root, p string) bool {
	root = filepath.Clean(root)
	p = filepath.Clean(p)
	if p == root {
		return true
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func bwrapExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	return -1
}

var _ ports.SandboxBackend = BwrapBackend{}
