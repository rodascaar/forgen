// Package tools implementa el puerto ToolExecutor con herramientas
// agnósticas al lenguaje, construidas sobre los puertos FileSystem/Executor/Git.
package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// schema helpers para construir JSON Schema de forma concisa.

func objectSchema(properties map[string]any, required ...string) map[string]any {
	props := make(map[string]map[string]any, len(properties))
	for k, v := range properties {
		if m, ok := v.(map[string]any); ok {
			props[k] = m
		} else {
			// Assume it's already a property schema (map[string]any with type/description)
			// Wrap it in the expected format
			props[k] = map[string]any{"type": "string", "description": fmt.Sprintf("%v", v)}
		}
	}
	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringProp(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func intProp(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func boolProp(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

// decodeArgs convierte los argumentos tipados del modelo a un struct destino.
func decodeArgs[ArgType any](raw map[string]any) (ArgType, error) {
	var args ArgType
	// La marshalling/unmarshalling JSON valida que los tipos coincidan.
	data, err := json.Marshal(raw)
	if err != nil {
		return args, fmt.Errorf("serializar argumentos: %w", err)
	}
	if err := json.Unmarshal(data, &args); err != nil {
		return args, fmt.Errorf("argumentos inválidos para la herramienta: %w", err)
	}
	return args, nil
}

// truncate limita la salida para no saturar el contexto del modelo.
func truncate(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	return text[:maxChars] + fmt.Sprintf("\n... [salida truncada: %d caracteres]", len(text)-maxChars)
}

func summarizeResult(output string, maxChars int) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return "(sin salida)"
	}
	return truncate(output, maxChars)
}
