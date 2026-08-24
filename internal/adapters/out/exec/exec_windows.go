//go:build windows

package exec

import (
	"os/exec"
)

// setProcessGroup no aplica en Windows (no hay grupos de procesos POSIX); la
// cancelación se maneja con taskkill /T en killProcessGroup.
func setProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup termina el comando y todo su árbol de procesos con taskkill.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return exec.Command("taskkill", "/PID", itoa(cmd.Process.Pid), "/T", "/F").Run()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
