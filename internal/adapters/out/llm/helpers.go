package llm

import (
	"encoding/json"
	"strings"
)

// parseArguments parsea la cadena JSON cruda de argumentos (acumulada durante
// el streaming) en el mapa destino. Una cadena vacía es válida (sin argumentos).
func parseArguments(raw string, target map[string]any) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return json.Unmarshal([]byte(raw), &target)
}

// normalizeReasoningEffort devuelve el nivel de razonamiento válido para enviar
// al proveedor. Vacío u "off" se traduce a "" (no enviar). low/medium/high se
// conservan; cualquier otro valor se ignora.
func normalizeReasoningEffort(effort string) string {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "", "off":
		return ""
	case "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(effort))
	default:
		return ""
	}
}
