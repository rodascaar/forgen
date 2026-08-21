// Package skills implementa el descubrimiento e inyección de habilidades
// (skills) definidas como directorios con un SKILL.md.
package skills

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/rodascaar/forgen/internal/core/ports"
)

// Skill es una habilidad reutilizable con instrucciones.
type Skill struct {
	Name        string
	Description string
	Body        string
}

// Discover escanea los directorios de skills y devuelve las habilidades
// ordenadas por nombre. Cada skill es un subdirectorio con un SKILL.md.
func Discover(ctx context.Context, dirs []string, fs ports.FileSystem) ([]Skill, error) {
	seen := map[string]Skill{}
	for _, dir := range dirs {
		entries, err := fs.Glob(ctx, dir+"/**/SKILL.md")
		if err != nil {
			continue // directorio inexistente no es error fatal
		}
		for _, path := range entries {
			data, err := fs.Read(ctx, path)
			if err != nil {
				continue
			}
			skill := ParseSkill(string(data), path)
			if skill.Name == "" {
				continue
			}
			// La primera aparición gana (global antes que projecto).
			if _, exists := seen[skill.Name]; !exists {
				seen[skill.Name] = skill
			}
		}
	}

	result := make([]Skill, 0, len(seen))
	for _, skill := range seen {
		result = append(result, skill)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// ParseSkill extrae nombre, descripción y cuerpo del SKILL.md.
// El frontmatter es opcional (delimitado por --- con name/description).
func ParseSkill(content, path string) Skill {
	name := ""
	description := ""
	body := content

	if strings.HasPrefix(strings.TrimSpace(content), "---") {
		lines := strings.Split(content, "\n")
		frontmatter := map[string]string{}
		inFrontmatter := false
		closed := false
		bodyStart := 0
		for index, line := range lines {
			trimmed := strings.TrimSpace(line)
			if index == 0 && trimmed == "---" {
				inFrontmatter = true
				continue
			}
			if inFrontmatter && trimmed == "---" {
				closed = true
				bodyStart = index + 1
				break
			}
			if inFrontmatter {
				if key, value, ok := strings.Cut(trimmed, ":"); ok {
					frontmatter[strings.TrimSpace(key)] = strings.TrimSpace(value)
				}
			}
		}
		if closed {
			body = strings.Join(lines[bodyStart:], "\n")
			name = frontmatter["name"]
			description = frontmatter["description"]
		}
	}

	// Sin frontmatter: inferir nombre del directorio y descripción del primer
	// párrafo no vacío.
	if name == "" {
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) >= 2 {
			name = parts[len(parts)-2]
		}
	}
	if description == "" {
		description = firstParagraph(body)
	}
	return Skill{Name: name, Description: description, Body: strings.TrimSpace(body)}
}

func firstParagraph(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			if len(trimmed) > 120 {
				trimmed = trimmed[:120] + "..."
			}
			return trimmed
		}
	}
	return ""
}

// Catalog renderiza el listado de skills para el system prompt.
func Catalog(skills []Skill) string {
	if len(skills) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("Habilidades disponibles (usa la herramienta read_skill para ver el detalle):\n")
	for _, skill := range skills {
		fmt.Fprintf(&builder, "- %s: %s\n", skill.Name, skill.Description)
	}
	return strings.TrimSpace(builder.String())
}

// ResolveSkill devuelve el cuerpo completo de una skill por nombre.
func ResolveSkill(skills []Skill, name string) (Skill, bool) {
	for _, skill := range skills {
		if skill.Name == name {
			return skill, true
		}
	}
	return Skill{}, false
}
