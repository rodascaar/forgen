// Package exec implementa ports.Executor sobre el shell local.
package exec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/rodascaar/forgen/internal/core/ports"
)

// maxOutputBytes limita la salida capturada de un comando.
const maxOutputBytes = 512 * 1024

func isWindows() bool { return runtime.GOOS == "windows" }

// LocalExecutor ejecuta comandos con /bin/sh (macOS/Linux) o cmd.exe (Windows).
type LocalExecutor struct {
	// DefaultWorkdir es el directorio donde se ejecuta si no se especifica.
	DefaultWorkdir string
}

// New construye un ejecutor local.
func New(defaultWorkdir string) *LocalExecutor {
	return &LocalExecutor{DefaultWorkdir: defaultWorkdir}
}

// Execute implementa ports.Executor.
func (l *LocalExecutor) Execute(ctx context.Context, command, workdir string, env []string) (ports.ExecResult, error) {
	if strings.TrimSpace(command) == "" {
		return ports.ExecResult{}, fmt.Errorf("comando vacío")
	}
	if workdir == "" {
		workdir = l.DefaultWorkdir
	}

	var cmd *exec.Cmd
	if isWindows() {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "/bin/sh", "-c", command)
	}
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), env...)

	var stdout, stderr limitedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Ejecutar en un grupo de procesos propio para poder matar TODO el árbol
	// (p.ej. `docker compose up` y sus hijos) al cancelar el contexto, y no
	// dejar procesos huérfanos que mantengan abiertos stdout/stderr y cuelguen
	// cmd.Run() indefinidamente.
	setProcessGroup(cmd)
	cmd.Cancel = func() error {
		return killProcessGroup(cmd)
	}
	// Si el proceso no muere tras la cancelación, no bloquear el caller.
	cmd.WaitDelay = 2 * time.Second

	err := cmd.Run()
	result := ports.ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode(err),
	}
	if err != nil && result.ExitCode == 0 {
		// Error de arranque (comando no encontrado), no de salida.
		return result, fmt.Errorf("no se pudo ejecutar %q: %w", command, err)
	}
	return result, nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return exitError.ExitCode()
	}
	return -1
}

// limitedBuffer captura la salida con un límite duro.
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

var _ ports.Executor = (*LocalExecutor)(nil)
