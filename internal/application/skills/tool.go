package skills

import (
	"context"
	"fmt"

	"github.com/forgen/forgen/internal/application/tools"
	"github.com/forgen/forgen/internal/core/domain"
)

// NewReadSkillTool construye la herramienta read_skill que devuelve el cuerpo
// completo de una habilidad por nombre.
func NewReadSkillTool(resolve func(name string) (Skill, bool)) tools.ToolDef {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string", "description": "Nombre de la habilidad a leer"},
		},
		"required": []string{"name"},
	}
	return tools.ToolDef{
		Tool: domain.Tool{
			Name:        "read_skill",
			Description: "Lee el contenido completo de una habilidad (skill) por su nombre.",
			Status:      domain.ToolStatusEnabled,
			Schema:      schema,
		},
		Execute: func(ctx context.Context, raw map[string]any) domain.ToolResult {
			name, _ := raw["name"].(string)
			if name == "" {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("read_skill requiere 'name'")}
			}
			skill, ok := resolve(name)
			if !ok {
				return domain.ToolResult{OK: false, Error: fmt.Errorf("skill %q no encontrada", name)}
			}
			return domain.ToolResult{OK: true, Output: skill.Body}
		},
	}
}
