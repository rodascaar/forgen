// Package hook implementa hooks de bash que reescriben o bloquean comandos.
package hook

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rodascaar/forgen/internal/core/ports"
)

// Result es el resultado de aplicar los hooks a un comando.
type Result struct {
	Command string // comando (posiblemente reescrito)
	Blocked bool
	Reason  string
}

// Executor envuelve un ports.Executor y aplica hooks antes de ejecutar.
type Executor struct {
	inner    ports.Executor
	hookDirs []string
	logger   *slog.Logger
}

// NewExecutor construye el executor con hooks. hookDirs se revisa en orden.
func NewExecutor(inner ports.Executor, hookDirs []string, logger *slog.Logger) *Executor {
	return &Executor{inner: inner, hookDirs: hookDirs, logger: logger}
}

// Execute implementa ports.Executor aplicando hooks previos.
func (h *Executor) Execute(ctx context.Context, command, workdir string, env []string) (ports.ExecResult, error) {
	result := h.applyHooks(ctx, command)
	if result.Blocked {
		return ports.ExecResult{}, fmt.Errorf("comando bloqueado por hook: %s", result.Reason)
	}
	return h.inner.Execute(ctx, result.Command, workdir, env)
}

// applyHooks recorre los hooks (ordenados) y aplica reescritura/bloqueo.
func (h *Executor) applyHooks(ctx context.Context, command string) Result {
	current := command
	for _, dir := range h.hookDirs {
		for _, script := range listExecutables(dir) {
			rewritten, blocked, reason := runHook(ctx, script, current)
			if blocked {
				h.logger.Info("hook.blocked", "script", script, "reason", reason)
				return Result{Command: current, Blocked: true, Reason: reason}
			}
			if rewritten != "" && rewritten != current {
				h.logger.Info("hook.rewrote", "script", script, "command", rewritten)
				current = rewritten
			}
		}
	}
	return Result{Command: current}
}

// listExecutables devuelve los scripts ejecutables de un directorio (ordenados).
func listExecutables(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	scripts := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err == nil && info.Mode()&0o111 != 0 {
			scripts = append(scripts, path)
		}
	}
	sort.Strings(scripts)
	return scripts
}

// runHook ejecuta un script: recibe el comando por stdin y devuelve el
// comando (posiblemente reescrito) por stdout. Exit code != 0 bloquea.
func runHook(ctx context.Context, script, command string) (rewritten string, blocked bool, reason string) {
	cmd := exec.CommandContext(ctx, script)
	cmd.Stdin = strings.NewReader(command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		reason := strings.TrimSpace(stderr.String())
		if reason == "" {
			reason = err.Error()
		}
		return "", true, reason
	}
	return strings.TrimSpace(stdout.String()), false, ""
}
