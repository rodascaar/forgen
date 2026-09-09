// Package sandbox aísla la ejecución de comandos del agente a nivel de SO.
//
// Backends (misma interfaz ports.SandboxBackend, reemplazable sin tocar el agente):
//   - seatbelt (macOS): /usr/bin/sandbox-exec, incluido en el SO, sin instalar.
//   - bwrap (Linux): bubblewrap del sistema si está en PATH.
//   - docker (contenedor): backend legacy opt-in.
//
// Sin backend disponible se degrada con warning a ejecución local (los approvals
// siguen aplicando: defensa en profundidad, mismo trato que Codex ante kernels
// sin userns). Modos: read-only / workspace-write (default) / danger-full-access.
package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// maxOutputBytes limita stdout/stderr capturados (512 KiB, paridad exec/docker).
const maxOutputBytes = 512 * 1024

// limitedBuffer captura la salida con un límite duro (compartido por backends).
type limitedBuffer struct {
	buffer bytes.Buffer
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	remaining := maxOutputBytes - b.buffer.Len()
	if remaining <= 0 {
		return len(data), nil
	}
	if len(data) > remaining {
		data = data[:remaining]
	}
	return b.buffer.Write(data)
}

func (b *limitedBuffer) String() string { return b.buffer.String() }

// NativeExecutor implementa ports.Executor confinando vía el mejor backend
// disponible para la plataforma. Es lo que cablea buildExecutor.
type NativeExecutor struct {
	policy   domain.SandboxPolicy
	fallback ports.Executor
	backend  ports.SandboxBackend
	logger   *slog.Logger
	degraded error
}

// NewNativeExecutor construye el executor confinado para un workspace y modo.
// Si ningún backend está disponible, degrada a fallback con warning (no falla:
// los approvals siguen protegiendo).
func NewNativeExecutor(workspace string, mode domain.SandboxMode, allowNetwork bool, extraWritable []string, fallback ports.Executor, logger *slog.Logger) *NativeExecutor {
	if logger == nil {
		logger = slog.Default()
	}
	policy := domain.SandboxPolicy{
		Mode:          mode,
		Workspace:     filepath.Clean(workspace),
		AllowNetwork:  allowNetwork,
		ReadOnlyPaths: []string{filepath.Join(filepath.Clean(workspace), ".git")},
		ExtraWritable: extraWritable,
	}
	e := &NativeExecutor{policy: policy, fallback: fallback, logger: logger}
	backend := selectBackend(runtime.GOOS)
	if backend == nil {
		e.degraded = fmt.Errorf("sandbox: sin backend para %s (experimental) — degradado a ejecución local con approvals", runtime.GOOS)
		logger.Warn("sandbox.degraded", "goos", runtime.GOOS)
		return e
	}
	if err := backend.Available(context.Background()); err != nil {
		e.degraded = err
		logger.Warn("sandbox.degraded", "backend", backend.Name(), "err", err)
		return e
	}
	e.backend = backend
	e.policy.Enforced = true
	e.policy.Backend = backend.Name()
	logger.Info("sandbox.active", "backend", backend.Name(), "mode", string(mode), "network", allowNetwork)
	return e
}

// selectBackend elige backend por SO. Función pura para testabilidad.
// El backend darwin vive en platform_darwin.go (build tag) para no romper
// la compilación en linux/windows.
func selectBackend(goos string) ports.SandboxBackend {
	if b := platformBackend(goos); b != nil {
		return b
	}
	switch goos {
	case "linux":
		return BwrapBackend{}
	default:
		return nil
	}
}

// Policy expone la política efectiva (para inyectarla al system prompt).
func (e *NativeExecutor) Policy() domain.SandboxPolicy { return e.policy }

// Describe resume backend/modo/red para `forgen debug sandbox` y doctor.
func (e *NativeExecutor) Describe() string {
	status := "enforced via " + e.policy.Backend
	if !e.policy.Enforced {
		status = "DEGRADED: " + e.degradedReason()
	}
	return "backend=" + backendOrNone(e) + " mode=" + string(e.policy.Mode) +
		" network=" + boolString(e.policy.AllowNetwork) + " " + status +
		"\nworkspace=" + e.policy.Workspace
}

// ProfileText expone el perfil efectivo cuando el backend lo soporta (debug).
func (e *NativeExecutor) ProfileText() string { return profileTextForPolicy(e.policy) }

func (e *NativeExecutor) degradedReason() string {
	if e.degraded != nil {
		return e.degraded.Error()
	}
	return "sin backend"
}

func backendOrNone(e *NativeExecutor) string {
	if e.policy.Backend == "" {
		return "none"
	}
	return e.policy.Backend
}

func boolString(b bool) string {
	if b {
		return "allow"
	}
	return "deny"
}

// Execute implementa ports.Executor.
func (e *NativeExecutor) Execute(ctx context.Context, command, workdir string, env []string) (ports.ExecResult, error) {
	if strings.TrimSpace(command) == "" {
		return ports.ExecResult{}, fmt.Errorf("comando vacío")
	}
	if e.policy.Mode == domain.SandboxFullAccess || e.backend == nil {
		if e.degraded != nil {
			e.logger.Warn("sandbox.degraded_execute", "err", e.degraded)
		}
		return e.fallback.Execute(ctx, command, workdir, env)
	}
	if workdir == "" {
		workdir = e.policy.Workspace
	}
	env = withSandboxEnv(env, e.policy)
	return e.backend.Execute(ctx, e.policy, command, workdir, env)
}

// withSandboxEnv redirige caches de toolchain al workspace: bajo la jaula,
// GOCACHE/TMP fuera del workspace deniegan escritura y `go build` fallaría.
// Verificado empíricamente en macOS/Seatbelt.
func withSandboxEnv(env []string, policy domain.SandboxPolicy) []string {
	cacheDir := filepath.Join(policy.Workspace, ".forgen", "sb-cache")
	_ = os.MkdirAll(filepath.Join(cacheDir, "gocache"), 0755)
	_ = os.MkdirAll(filepath.Join(cacheDir, "tmp"), 0755)
	has := func(key string) bool {
		prefix := key + "="
		for _, kv := range env {
			if strings.HasPrefix(kv, prefix) {
				return true
			}
		}
		return false
	}
	if !has("GOCACHE") {
		env = append(env, "GOCACHE="+filepath.Join(cacheDir, "gocache"))
	}
	if !has("GOTMPDIR") {
		env = append(env, "GOTMPDIR="+filepath.Join(cacheDir, "tmp"))
	}
	return env
}

var _ ports.Executor = (*NativeExecutor)(nil)
