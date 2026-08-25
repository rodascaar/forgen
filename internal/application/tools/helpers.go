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
// Si el texto parece output de tests con muchos "PASS"/"ok", preserva solo failures para ahorrar tokens (9B).
func truncate(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	// Heurística: si es output de tests/lint con muchos PASS, extrae solo failures/stacktrace.
	if isTestOutput(text) {
		filtered := filterTestFailures(text, maxChars)
		if len(filtered) < len(text) && len(filtered) <= maxChars+2000 {
			return filtered + fmt.Sprintf("\n... [filtrado: %d→%d chars, solo failures]", len(text), len(filtered))
		}
	}
	return text[:maxChars] + fmt.Sprintf("\n... [salida truncada: %d caracteres — usa read con offset/limit o grep para paginar]", len(text)-maxChars)
}

func isTestOutput(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "pass") || strings.Contains(lower, "fail") || strings.Contains(lower, "---") || strings.Contains(lower, "ok ")
}

func filterTestFailures(text string, maxChars int) string {
	lines := strings.Split(text, "\n")
	var kept []string
	for _, l := range lines {
		low := strings.ToLower(l)
		if strings.Contains(low, "fail") || strings.Contains(low, "error") || strings.Contains(low, "panic") || strings.Contains(low, "---") {
			kept = append(kept, l)
		}
	}
	if len(kept) == 0 {
		// fallback: head + tail
		if len(lines) > 40 {
			kept = append(lines[:20], lines[len(lines)-20:]...)
		} else {
			kept = lines
		}
	}
	out := strings.Join(kept, "\n")
	if len(out) > maxChars {
		return out[:maxChars]
	}
	return out
}

func summarizeResult(output string, maxChars int) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return "(sin salida)"
	}
	return truncate(output, maxChars)
}
