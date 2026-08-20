// Package sandbox implementa la ejecución de comandos dentro de un contenedor.
package sandbox

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	"github.com/forgen/forgen/internal/core/ports"
)

// DockerExecutor ejecuta comandos dentro de un contenedor Docker.
type DockerExecutor struct {
	image     string
	workspace string
}

// NewDockerExecutor construye el executor de sandbox docker.
func NewDockerExecutor(image, workspace string) *DockerExecutor {
	return &DockerExecutor{image: image, workspace: workspace}
}

// buildArgs construye los argumentos de `docker run`.
func (d *DockerExecutor) BuildArgs(command string) []string {
	return []string{
		"run", "--rm",
		"--network", "none",
		"-v", d.workspace + ":/workspace",
		"-w", "/workspace",
		d.image,
		"sh", "-c", command,
	}
}

// Execute implementa ports.Executor.
func (d *DockerExecutor) Execute(ctx context.Context, command, workdir string, env []string) (ports.ExecResult, error) {
	args := d.BuildArgs(command)
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := ports.ExecResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: 0,
	}
	if err != nil {
		var exitError *exec.ExitError
		if ok := errors.As(err, &exitError); ok {
			result.ExitCode = exitError.ExitCode()
		} else {
			return result, err
		}
	}
	return result, nil
}

var _ ports.Executor = (*DockerExecutor)(nil)
