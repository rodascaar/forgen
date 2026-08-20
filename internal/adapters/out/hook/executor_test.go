package hook_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/forgen/forgen/internal/adapters/out/hook"
	"github.com/forgen/forgen/internal/core/ports"
)

type passthroughExecutor struct {
	lastCommand string
}

func (p *passthroughExecutor) Execute(_ context.Context, command, workdir string, env []string) (ports.ExecResult, error) {
	p.lastCommand = command
	return ports.ExecResult{Stdout: command, ExitCode: 0}, nil
}

func writeHook(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestHookRewritesCommand(t *testing.T) {
	dir := t.TempDir() + "/hooks/bash"
	// Hook que antepone "echo rewrote; " al comando.
	writeHook(t, dir, "10-rewrite.sh", `#!/bin/sh
read cmd
echo "safe-command: $cmd"
`)

	inner := &passthroughExecutor{}
	executor := hook.NewExecutor(inner, []string{dir}, slog.Default())

	_, err := executor.Execute(context.Background(), "original", "/tmp", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if inner.lastCommand != "safe-command: original" {
		t.Fatalf("comando reescrito = %q", inner.lastCommand)
	}
}

func TestHookBlocksCommand(t *testing.T) {
	dir := t.TempDir() + "/hooks/bash"
	writeHook(t, dir, "20-block.sh", `#!/bin/sh
read cmd
echo "comando prohibido" >&2
exit 1
`)

	inner := &passthroughExecutor{}
	executor := hook.NewExecutor(inner, []string{dir}, slog.Default())

	_, err := executor.Execute(context.Background(), "rm -rf /", "/tmp", nil)
	if err == nil {
		t.Fatal("esperaba bloqueo del comando")
	}
	if inner.lastCommand != "" {
		t.Fatal("el comando no debería ejecutarse si se bloqueó")
	}
}

func TestNoHooksPassThrough(t *testing.T) {
	inner := &passthroughExecutor{}
	executor := hook.NewExecutor(inner, []string{t.TempDir()}, slog.Default())

	_, err := executor.Execute(context.Background(), "ls", "/tmp", nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if inner.lastCommand != "ls" {
		t.Fatalf("comando = %q, want ls", inner.lastCommand)
	}
}
