package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rodascaar/forgen/internal/core/ports"
)

// contextFileName es el nombre por defecto de las instrucciones de proyecto.
const contextFileName = "AGENTS.md"

// alternativeContextFileName es el nombre alternativo por compatibilidad.
const alternativeContextFileName = "CLAUDE.md"

// contextSectionsMaxChars limita el contexto inyectado en el system prompt.
const contextSectionsMaxChars = 24000

// ContextBlock es una sección de contexto con su origen.
type ContextBlock struct {
	Title    string
	Content  string
	Priority int // mayor = se inyecta más tarde (sobrescribe)
}

// LoadProjectContext descubre los archivos de contexto desde el workspace
// hacia la raíz y devuelve los bloques ordenados por prioridad.
func LoadProjectContext(ctx context.Context, workspace string, fs ports.FileSystem) ([]ContextBlock, error) {
	absolute, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolver workspace: %w", err)
	}

	var blocks []ContextBlock
	current := absolute
	for {
		block, found, err := readContextFile(ctx, current, fs)
		if err != nil {
			return nil, err
		}
		if found {
			blocks = append(blocks, block)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	// Invertir: la raíz primero, el workspace al final (mayor prioridad).
	sort.SliceStable(blocks, func(i, j int) bool { return blocks[i].Priority < blocks[j].Priority })
	return blocks, nil
}

func readContextFile(ctx context.Context, dir string, fs ports.FileSystem) (ContextBlock, bool, error) {
	// AGENTS.md tiene prioridad sobre CLAUDE.md en el mismo directorio.
	for _, name := range []string{contextFileName, alternativeContextFileName} {
		path := filepath.Join(dir, name)
		exists, err := fs.Exists(ctx, path)
		if err != nil {
			return ContextBlock{}, false, fmt.Errorf("verificar %s: %w", path, err)
		}
		if !exists {
			continue
		}
		data, err := fs.Read(ctx, path)
		if err != nil {
			return ContextBlock{}, false, fmt.Errorf("leer %s: %w", path, err)
		}
		return ContextBlock{Title: path, Content: string(data), Priority: len(dir)}, true, nil
	}
	return ContextBlock{}, false, nil
}

// ComposeSystemPrompt une el prompt del agente con los bloques de contexto.
func ComposeSystemPrompt(agentSystemPrompt string, blocks []ContextBlock, toolchain string) string {
	var builder strings.Builder
	builder.WriteString(agentSystemPrompt)
	builder.WriteString("\n\n")

	if toolchain != "" {
		builder.WriteString("## Entorno del proyecto\n")
		builder.WriteString(toolchain)
		builder.WriteString("\n\n")
	}

	total := builder.Len()
	for _, block := range blocks {
		content := strings.TrimSpace(block.Content)
		if content == "" {
			continue
		}
		section := fmt.Sprintf("## Instrucciones de proyecto (%s)\n%s\n", block.Title, content)
		if total+len(section) > contextSectionsMaxChars {
			break
		}
		builder.WriteString(section)
		total += len(section)
	}

	return strings.TrimSpace(builder.String())
}

// LoadToolchainContext detecta el lenguaje y toolchain del proyecto.
func LoadToolchainContext(ctx context.Context, workspace string,
	detector ports.LanguageDetector, probe ports.ToolchainProbe) (string, error) {
	language := ""
	var detectErr error
	if detector != nil {
		language, detectErr = detector.Detect(ctx, workspace)
	}
	toolchain := ""
	if probe != nil {
		var probeErr error
		toolchain, probeErr = probe.Probe(ctx, workspace)
		if probeErr != nil {
			return "", fmt.Errorf("detectar toolchain: %w", probeErr)
		}
	}
	var builder strings.Builder
	if language != "" {
		fmt.Fprintf(&builder, "Lenguaje principal: %s\n", language)
	}
	if detectErr != nil {
		fmt.Fprintf(&builder, "Detección de lenguaje: %v\n", detectErr)
	}
	if toolchain != "" {
		builder.WriteString(toolchain)
	}
	return strings.TrimSpace(builder.String()), nil
}
