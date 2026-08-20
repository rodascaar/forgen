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
