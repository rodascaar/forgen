//go:build darwin

// Package sandbox — backend Seatbelt (macOS).
//
// Usa /usr/bin/sandbox-exec, presente en el SO sin instalación (misma apuesta
// que Codex CLI). El perfil SBPL se genera en memoria por política y se pasa
// con -p. Lo deprecado es el frontend sandbox-exec, no el motor Seatbelt;
// si Apple lo retira, se añade un backend nuevo sin tocar el agente.
package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

// maxOutputBytes vive en native.go (compartido con bwrap/docker).

// SeatbeltBackend impone SandboxPolicy vía sandbox-exec (macOS).
type SeatbeltBackend struct{}

func (SeatbeltBackend) Name() string { return "seatbelt" }

// Available verifica el binario del SO con una ejecución real mínima.
func (SeatbeltBackend) Available(ctx context.Context) error {
	if _, err := os.Stat("/usr/bin/sandbox-exec"); err != nil {
		return fmt.Errorf("seatbelt: /usr/bin/sandbox-exec no disponible: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/sandbox-exec", "-p", "(version 1)(allow default)", "/usr/bin/true")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("seatbelt: prueba sandbox-exec falló: %w", err)
	}
	return nil
}

// Execute corre el comando confinado. workdir debe estar dentro del workspace.
func (SeatbeltBackend) Execute(ctx context.Context, policy domain.SandboxPolicy, command, workdir string, env []string) (ports.ExecResult, error) {
	if strings.TrimSpace(command) == "" {
		return ports.ExecResult{}, fmt.Errorf("seatbelt: comando vacío")
	}
	if workdir == "" {
		workdir = policy.Workspace
	}
	if !pathInside(policy.Workspace, workdir) {
		return ports.ExecResult{}, fmt.Errorf("seatbelt: workdir %q fuera del workspace %q", workdir, policy.Workspace)
	}
	profile := BuildSeatbeltProfile(policy)
	cmd := exec.CommandContext(ctx, "/usr/bin/sandbox-exec", "-p", profile, "/bin/sh", "-c", command)
	cmd.Dir = workdir
	cmd.Env = append(os.Environ(), env...)
	// Grupo propio para matar todo el árbol al cancelar (paridad LocalExecutor).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
	cmd.WaitDelay = 2 * time.Second

	var stdout, stderr limitedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := ports.ExecResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode(err)}
	if err != nil && result.ExitCode == 0 {
		return result, fmt.Errorf("seatbelt: no se pudo ejecutar: %w", err)
	}
	return result, nil
}

// BuildSeatbeltProfile genera el perfil SBPL para la política.
// Deny-by-default con carveouts: lecturas amplias, escritura solo workspace,
// subrutas read-only (.git) DESPUÉS de los allows (Seatbelt: last-match wins),
// red binaria (omitir = denegar).
// Los paths se canonicalizan con EvalSymlinks: /tmp es symlink a /private/tmp
// y las regex deben casar el path resuelto (verificado empíricamente).
func BuildSeatbeltProfile(policy domain.SandboxPolicy) string {
	ws := canonicalPath(policy.Workspace)
	var sb strings.Builder
	sb.WriteString("(version 1)\n")
	sb.WriteString("(deny default)\n")
	// Lectura amplia (necesaria para toolchain, sh, go, node...).
	sb.WriteString("(allow file-read*)\n")
	allowWrite := func(pattern string) {
		// OJO: no usar %q aquí — escaparía la contrabarra de `\.` a `\\.`
		// y la regex dejaría de casar `.git` (bug verificado empíricamente).
		sb.WriteString("(allow file-write* (regex #\"" + pattern + "\"))\n")
	}
	denyWrite := func(pattern string) {
		sb.WriteString("(deny file-write* (regex #\"" + pattern + "\"))\n")
	}
	switch policy.Mode {
	case domain.SandboxReadOnly:
		// Sin escritura en ningún path (deny default ya la bloquea).
	case domain.SandboxFullAccess:
		sb.WriteString("(allow file-write*)\n")
	default: // workspace-write
		allowWrite(regexPathPrefix(ws))
		for _, extra := range policy.ExtraWritable {
			if e := strings.TrimSpace(extra); e != "" {
				allowWrite(regexPathPrefix(canonicalPath(e)))
			}
		}
		// Scratch para herramientas (compiladores, tests). Se listan literales y
		// resueltos: /tmp→private/tmp y /var→private/var son symlinks y Seatbelt
		// casa ambas formas según el caso (verificado empíricamente).
		sb.WriteString("(allow file-write* (subpath \"/tmp\") (subpath \"/private/tmp\") (subpath \"/var/folders\") (subpath \"/private/var/folders\"))\n")
		sb.WriteString("(allow file-write* (subpath \"/dev\"))\n")
	}
	// Read-only siempre DESPUÉS de los allows (last-match wins).
	for _, ro := range policy.ReadOnlyPaths {
		denyWrite(regexPathPrefix(canonicalPath(ro)))
	}
	// Ejecución y primitivas del SO.
	sb.WriteString("(allow process-exec* (subpath \"/\"))\n")
	sb.WriteString("(allow process-fork)\n")
	sb.WriteString("(allow signal (target self))\n")
	sb.WriteString("(allow sysctl-read)\n")
	sb.WriteString("(allow mach-lookup)\n")
	sb.WriteString("(allow ipc-posix-shm*)\n")
	sb.WriteString("(allow file-ioctl)\n")
	if policy.AllowNetwork {
		sb.WriteString("(allow network*)\n")
	}
	return sb.String()
}

// canonicalPath resuelve symlinks (/tmp → /private/tmp) para que las regex
// casen el path que ve el kernel. Fallback a Clean si no existe aún.
func canonicalPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}

// regexPathPrefix devuelve regex SBPL que casa el path y sus hijos.
// Sin comillas dobles (se inyecta en literal #"..." sin escapar).
func regexPathPrefix(path string) string {
	path = strings.ReplaceAll(path, `"`, "")
	var sb strings.Builder
	sb.WriteString("^")
	for _, r := range path {
		if strings.ContainsRune(`\.+*?()|[]{}^$`, r) {
			sb.WriteRune('\\')
		}
		sb.WriteRune(r)
	}
	sb.WriteString("(/|$)")
	return sb.String()
}

// pathInside indica si p está dentro de root (o es root).
func pathInside(root, p string) bool {
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

// limitedBuffer vive en native.go (compartido por backends).

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

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	return nil
}

var _ ports.SandboxBackend = SeatbeltBackend{}
