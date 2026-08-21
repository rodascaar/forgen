// Package git implementa ports.Git sobre el binario git del sistema.
package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/rodascaar/forgen/internal/core/ports"
)

// CLI ejecuta git del sistema con salida porcelain.
type CLI struct{}

// New construye el adapter de git.
func New() *CLI { return &CLI{} }

func (c *CLI) run(ctx context.Context, workdir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = workdir
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

// Status implementa ports.Git.
func (c *CLI) Status(ctx context.Context, workdir string) (string, error) {
	return c.run(ctx, workdir, "status", "--porcelain")
}

// Diff implementa ports.Git.
func (c *CLI) Diff(ctx context.Context, workdir string, staged bool) (string, error) {
	args := []string{"diff"}
	if staged {
		args = append(args, "--cached")
	}
	return c.run(ctx, workdir, args...)
}

// IsRepo implementa ports.Git.
func (c *CLI) IsRepo(ctx context.Context, workdir string) (bool, error) {
	output, err := c.run(ctx, workdir, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return false, nil // no es repo, no es un error de la aplicación
	}
	return output == "true", nil
}

var _ ports.Git = (*CLI)(nil)
