package tui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/atotto/clipboard"
)

// atottoCopy es el fallback genérico previo de forgen.

// osc52MaxRawBytes evita saturar el terminal con payloads gigantes
// (mismo límite que Codex: 100_000 bytes crudos antes de base64).
const osc52MaxRawBytes = 100_000

// copyEnv describe el entorno para elegir el backend de portapapeles.
// Orden inspirado en codex clipboard_copy.rs y opencode clipboard.ts:
// SSH -> tmux/OSC52 (el clipboard nativo sería el del host remoto, inútil);
// local -> nativo -> WSL powershell -> tmux/OSC52.
type copyEnv struct {
	ssh  bool
	wsl  bool
	tmux bool
}

// detectCopyEnv lee el entorno real del proceso.
func detectCopyEnv() copyEnv {
	_, ssh := os.LookupEnv("SSH_TTY")
	if !ssh {
		_, ssh = os.LookupEnv("SSH_CONNECTION")
	}
	_, tmux := os.LookupEnv("TMUX")
	if !tmux {
		_, tmux = os.LookupEnv("TMUX_PANE")
	}
	return copyEnv{ssh: ssh, wsl: isWSLSession(), tmux: tmux}
}

func isWSLSession() bool {
	if _, ok := os.LookupEnv("WSL_DISTRO_NAME"); ok {
		return true
	}
	if _, ok := os.LookupEnv("WSL_INTEROP"); ok {
		return true
	}
	if data, err := os.ReadFile("/proc/version"); err == nil {
		lower := strings.ToLower(string(data))
		if strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl") {
			return true
		}
	}
	return false
}

// copyToClipboard copia texto al portapapeles eligiendo el mejor backend.
// Devuelve el nombre del método usado ("nativo", "wsl", "tmux", "OSC52")
// para mostrar feedback útil en la TUI.
func copyToClipboard(text string) (string, error) {
	env := detectCopyEnv()
	return copyWithEnv(text, env, nativeCopy, wslCopy, tmuxCopy, osc52Copy)
}

func copyWithEnv(
	text string,
	env copyEnv,
	nativeFn func(string) error,
	wslFn func(string) error,
	tmuxFn func(string) error,
	osc52Fn func(string) error,
) (string, error) {
	if env.ssh {
		// Sobre SSH el clipboard nativo pertenece a la máquina remota.
		if env.tmux {
			if err := tmuxFn(text); err == nil {
				return "tmux", nil
			} else if oscErr := osc52Fn(text); oscErr == nil {
				return "OSC52", nil
			} else {
				return "", fmt.Errorf("tmux: %v; OSC52: %v", err, oscErr)
			}
		}
		if err := osc52Fn(text); err != nil {
			return "", fmt.Errorf("OSC52 sobre SSH: %w", err)
		}
		return "OSC52", nil
	}
	if err := nativeFn(text); err == nil {
		return "nativo", nil
	} else {
		nativeErr := err
		if env.wsl {
			if wslErr := wslFn(text); wslErr == nil {
				return "wsl", nil
			} else {
				// Sigue a terminal-mediated como último recurso.
				if termErr := terminalCopy(text, env.tmux, tmuxFn, osc52Fn); termErr == nil {
					if env.tmux {
						return "tmux", nil
					}
					return "OSC52", nil
				} else {
					return "", fmt.Errorf("nativo: %v; WSL: %v; terminal: %v", nativeErr, wslErr, termErr)
				}
			}
		}
		if termErr := terminalCopy(text, env.tmux, tmuxFn, osc52Fn); termErr == nil {
			if env.tmux {
				return "tmux", nil
			}
			return "OSC52", nil
		} else {
			return "", fmt.Errorf("nativo: %v; terminal: %v", nativeErr, termErr)
		}
	}
}

func terminalCopy(text string, inTmux bool, tmuxFn, osc52Fn func(string) error) error {
	if inTmux {
		if err := tmuxFn(text); err == nil {
			return nil
		} else if oscErr := osc52Fn(text); oscErr == nil {
			return nil
		} else {
			return fmt.Errorf("tmux: %v; OSC52: %v", err, oscErr)
		}
	}
	return osc52Fn(text)
}

// nativeCopy intenta el clipboard del SO: wl-copy > xclip > xsel en Linux,
// pbcopy en macOS, powershell en Windows, y atotto/clipboard como último recurso.
func nativeCopy(text string) error {
	if path, err := exec.LookPath("wl-copy"); err == nil && os.Getenv("WAYLAND_DISPLAY") != "" {
		cmd := exec.Command(path)
		cmd.Stdin = strings.NewReader(text)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err == nil {
			return nil
		} else {
			lastNativeErr = fmt.Sprintf("wl-copy: %v %s", err, strings.TrimSpace(stderr.String()))
		}
	}
	if path, err := exec.LookPath("xclip"); err == nil {
		cmd := exec.Command(path, "-selection", "clipboard")
		cmd.Stdin = strings.NewReader(text)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err == nil {
			return nil
		} else {
			lastNativeErr = fmt.Sprintf("xclip: %v %s", err, strings.TrimSpace(stderr.String()))
		}
	}
	if path, err := exec.LookPath("xsel"); err == nil {
		cmd := exec.Command(path, "--clipboard", "--input")
		cmd.Stdin = strings.NewReader(text)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err == nil {
			return nil
		} else {
			lastNativeErr = fmt.Sprintf("xsel: %v %s", err, strings.TrimSpace(stderr.String()))
		}
	}
	if path, err := exec.LookPath("pbcopy"); err == nil {
		cmd := exec.Command(path)
		cmd.Stdin = strings.NewReader(text)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err == nil {
			return nil
		} else {
			lastNativeErr = fmt.Sprintf("pbcopy: %v %s", err, strings.TrimSpace(stderr.String()))
		}
	}
	// Fallback genérico (atotto usa pbcopy/xclip internamente según plataforma).
	if err := atottoCopy(text); err != nil {
		if lastNativeErr != "" {
			return fmt.Errorf("%s; clipboard: %v", lastNativeErr, err)
		}
		return err
	}
	return nil
}

// lastNativeErr guarda el último error de backend explícito para encadenarlo
// en el mensaje final (evita el genérico "Error copiando" sin contexto).
var lastNativeErr string

func atottoCopy(text string) error {
	return clipboard.WriteAll(text)
}

// wslCopy envía el texto al clipboard de Windows desde WSL vía powershell.exe.
func wslCopy(text string) error {
	ps, err := exec.LookPath("powershell.exe")
	if err != nil {
		ps, err = exec.LookPath("powershell")
		if err != nil {
			return fmt.Errorf("powershell no encontrado: %w", err)
		}
	}
	cmd := exec.Command(ps, "-NoProfile", "-Command",
		"[Console]::InputEncoding = [System.Text.Encoding]::UTF8; $ErrorActionPreference = 'Stop'; $text = [Console]::In.ReadToEnd(); Set-Clipboard -Value $text")
	cmd.Stdin = strings.NewReader(text)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		out := strings.TrimSpace(stderr.String())
		if out == "" {
			return fmt.Errorf("powershell: %w", err)
		}
		return fmt.Errorf("powershell: %s", out)
	}
	return nil
}

// tmuxCopy usa la integración nativa de tmux con el clipboard externo.
func tmuxCopy(text string) error {
	if out, err := exec.Command("tmux", "show-options", "-gv", "set-clipboard").Output(); err == nil {
		if strings.TrimSpace(string(out)) == "off" {
			return fmt.Errorf("tmux set-clipboard está en off (actívalo con `set -g set-clipboard on`)")
		}
	} else {
		return fmt.Errorf("tmux no disponible: %v", err)
	}
	cmd := exec.Command("tmux", "load-buffer", "-w", "-")
	cmd.Stdin = strings.NewReader(text)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		out := strings.TrimSpace(stderr.String())
		if out == "" {
			return fmt.Errorf("tmux load-buffer: %w", err)
		}
		return fmt.Errorf("tmux load-buffer: %s", out)
	}
	return nil
}

// osc52Sequence construye la secuencia de escape OSC52 (con wrap tmux si aplica).
func osc52Sequence(text string, inTmux bool) (string, error) {
	if len(text) > osc52MaxRawBytes {
		return "", fmt.Errorf("contenido demasiado grande para OSC52 (%d bytes, máx %d)", len(text), osc52MaxRawBytes)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	if inTmux {
		return fmt.Sprintf("\x1bPtmux;\x1b\x1b]52;c;%s\x07\x1b\\", encoded), nil
	}
	return fmt.Sprintf("\x1b]52;c;%s\x07", encoded), nil
}

// osc52Copy escribe la secuencia al /dev/tty (llega al emulador aunque stdout
// esté redirigido por bubbletea) con fallback a stdout.
func osc52Copy(text string) error {
	_, inTmux := os.LookupEnv("TMUX")
	seq, err := osc52Sequence(text, inTmux)
	if err != nil {
		return err
	}
	if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		_, werr := tty.WriteString(seq)
		cerr := tty.Close()
		if werr == nil && cerr == nil {
			return nil
		}
	}
	if _, err := fmt.Fprint(os.Stdout, seq); err != nil {
		return fmt.Errorf("OSC52: %w", err)
	}
	return nil
}
