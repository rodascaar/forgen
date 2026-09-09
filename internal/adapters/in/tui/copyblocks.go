package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// codeBlock es un bloque cercado ```extraído del Markdown crudo del asistente.
// Se parsea desde el texto fuente (no del render con wrap/padding) para que
// la copia sea exacta, como pide codex#32204.
type codeBlock struct {
	lang string
	code string
}

// extractCodeBlocks devuelve los bloques ``` del texto en orden de aparición.
// Soporta ```lang y cierre ``` en su propia línea; ignora cercas sin cierre.
func extractCodeBlocks(text string) []codeBlock {
	var blocks []codeBlock
	lines := strings.Split(text, "\n")
	inBlock := false
	var lang string
	var current []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(trimmed, "```"); ok {
			if !inBlock {
				inBlock = true
				lang = strings.TrimSpace(after)
				current = nil
			} else {
				inBlock = false
				blocks = append(blocks, codeBlock{lang: lang, code: strings.Join(current, "\n")})
				lang = ""
				current = nil
			}
			continue
		}
		if inBlock {
			current = append(current, line)
		}
	}
	return blocks
}

// stripPromptChars quita prefijos de prompt ($, ❯, >) de cada línea para que el
// comando se pueda pegar directo en el shell sin tipear.
func stripPromptChars(code string) string {
	lines := strings.Split(code, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		for _, prefix := range []string{"$", "❯", ">"} {
			if rest, ok := strings.CutPrefix(trimmed, prefix); ok {
				trimmed = strings.TrimSpace(rest)
				break
			}
		}
		lines[i] = trimmed
	}
	// Recorta líneas vacías de bordes pero conserva las internas.
	start := 0
	for start < len(lines) && lines[start] == "" {
		start++
	}
	end := len(lines)
	for end > start && lines[end-1] == "" {
		end--
	}
	return strings.Join(lines[start:end], "\n")
}

// assistantHistory devuelve las respuestas del asistente de más reciente a más
// antigua (incluye el buffer vivo de streaming como elemento 0 si existe).
func (m *Model) assistantHistory() []string {
	var out []string
	if m.assistantBuffer != "" {
		out = append(out, m.assistantBuffer)
	}
	for _, line := range slices.Backward(m.transcript) {
		if line.kind == "assistant" {
			out = append(out, line.text)
		}
	}
	return out
}

// copyTextWithFallback copia al portapapeles y, si no hay backend disponible,
// guarda en un archivo temporal y devuelve su ruta para no perder el contenido.
func copyTextWithFallback(text string) (method string, fallbackFile string, err error) {
	if method, err := copyToClipboard(text); err == nil {
		return method, "", nil
	} else {
		copyErr := err
		if path, werr := writeCopyFallback(text); werr == nil {
			return "", path, fmt.Errorf("%w (guardado en %s)", copyErr, path)
		}
		return "", "", copyErr
	}
}

// writeCopyFallback guarda el texto en /tmp cuando no hay portapapeles
// (SSH sin OSC52, CI sin xclip, etc.) para no perder el contenido.
func writeCopyFallback(text string) (string, error) {
	path := filepath.Join(os.TempDir(), fmt.Sprintf("forgen-copy-%d.md", time.Now().UnixNano()))
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// shortPreview resume un bloque para listarlo en el picker de /copy code.
func shortPreview(s string, maxLen int) string {
	first := strings.TrimSpace(s)
	if idx := strings.Index(first, "\n"); idx >= 0 {
		first = strings.TrimSpace(first[:idx])
	}
	if len(first) > maxLen {
		return first[:maxLen] + "..."
	}
	if first == "" {
		return "(vacío)"
	}
	return first
}

func parseCopyIndex(arg string, max int) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(arg))
	if err != nil || n < 1 || n > max {
		return 0, fmt.Errorf("usa /copy code 1..%d", max)
	}
	return n - 1, nil
}
