//go:build !windows

package exec

import (
	"os/exec"
	"syscall"
	"time"
)

// setProcessGroup asigna al comando un grupo de procesos propio (Setpgid) para
// poder matar todo el árbol al cancelar.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup envía SIGTERM y luego SIGKILL a todo el grupo de procesos
// del comando (PID negativo), de modo que los hijos (docker, etc.) también
// mueran y no mantengan abiertos stdout/stderr.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pgid := -cmd.Process.Pid
	_ = syscall.Kill(pgid, syscall.SIGTERM)
	time.Sleep(300 * time.Millisecond)
	return syscall.Kill(pgid, syscall.SIGKILL)
}
