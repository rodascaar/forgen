package llm

import (
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"strings"

	"github.com/rodascaar/forgen/internal/core/domain"
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

// parseArgumentsTolerant intenta parsear JSON con reparación automática de
// errores comunes en modelos pequeños: comillas simples, trailing commas,
// comillas faltantes en keys, etc.
func parseArgumentsTolerant(raw string, target map[string]any) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out map[string]any
	// 1) Intento nativo
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		maps.Copy(target, out)
		return nil
	}
	// 2) Reparaciones comunes para modelos pequeños
	fixed := repairJSON(raw)
	out = nil
	if err := json.Unmarshal([]byte(fixed), &out); err == nil {
		maps.Copy(target, out)
		return nil
	}
	// 3) Fallback: intentar extraer primer objeto JSON válido
	if obj := extractFirstJSON(raw); obj != "" {
		out = nil
		if err := json.Unmarshal([]byte(obj), &out); err == nil {
			maps.Copy(target, out)
			return nil
		}
	}
	return json.Unmarshal([]byte(raw), &out) // devuelve error original para logging
}

// repairJSON aplica correcciones heurísticas a JSON malformado.
func repairJSON(s string) string {
	// Eliminar markdown code fences
	s = regexp.MustCompile("^```(?:json)?\\s*").ReplaceAllString(s, "")
	s = regexp.MustCompile("\\s*```$").ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	// Comillas simples -> dobles en keys ('path': -> "path":)
	s = regexp.MustCompile(`'([a-zA-Z_][a-zA-Z0-9_]*)'\s*:`).ReplaceAllString(s, `"$1":`)
	// Valores con comillas simples (: 'valor' -> : "valor")
	s = regexp.MustCompile(`:\s*'([^'\n]*)'`).ReplaceAllString(s, `:"$1"`)

	// Trailing commas antes de } o ]
	s = regexp.MustCompile(`,\s*}`).ReplaceAllString(s, "}")
	s = regexp.MustCompile(`,\s*]`).ReplaceAllString(s, "]")

	// Keys sin comillas al inicio de objeto o tras { , (evita doble-citar las ya citadas).
	// Solo aplica cuando la key NO está precedida por " (lookbehind manual vía grupos).
	s = regexp.MustCompile(`([{,]\s*)([a-zA-Z_][a-zA-Z0-9_]*)\s*:`).ReplaceAllString(s, `$1"$2":`)

	return strings.TrimSpace(s)
}

// extractFirstJSON extrae el primer objeto/array JSON balanceado de un texto.
func extractFirstJSON(s string) string {
	s = strings.TrimSpace(s)
	start := -1
	for i, c := range s {
		if c == '{' || c == '[' {
			start = i
			break
		}
	}
	if start == -1 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if !inString {
			switch c {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return s[start : i+1]
				}
			}
		}
	}
	return ""
}

// extractNameArgs extrae name y arguments de un objeto tool call JSON.
func extractNameArgs(obj string) (string, string) {
	var parsed struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(obj), &parsed); err == nil && parsed.Name != "" {
		return parsed.Name, string(parsed.Arguments)
	}
	return "", ""
}

// toolCallPattern regex para detectar tool calls en texto libre (JSON plano).
var toolCallPattern = regexp.MustCompile(`(?s)\{\s*"name"\s*:\s*"([^"]+)"\s*,\s*"arguments"\s*:\s*(\{.*?\})\s*\}`)

// xmlToolPattern regex para detectar tool calls estilo XML: <function=name>{args}
var xmlToolPattern = regexp.MustCompile(`(?s)<function=(\w+)>\s*(\{.*?\})\s*</function>`)

// extractToolCallsFromText busca tool calls en texto libre (modelos que "hablan" en lugar de emitir tool_calls).
// Soporta: JSON code blocks, JSON objects sueltos, XML-style <function=name>{args}
// Devuelve slice de (name, argumentsJSON) listos para parseArgumentsTolerant.
func extractToolCallsFromText(text string, availableTools map[string]bool) [][2]string {
	var results [][2]string

	// 1) JSON code blocks
	jsonBlocks := regexp.MustCompile("```json\\s*(\\{.*?\\})\\s*```").FindAllStringSubmatch(text, -1)
	for _, m := range jsonBlocks {
		if len(m) >= 2 {
			if name, args := extractNameArgs(m[1]); name != "" && availableTools[name] {
				results = append(results, [2]string{name, args})
			}
		}
	}

	// 2) JSON objects sueltos en texto
	objs := toolCallPattern.FindAllStringSubmatch(text, -1)
	for _, m := range objs {
		if len(m) >= 3 && availableTools[m[1]] {
			results = append(results, [2]string{m[1], m[2]})
		}
	}

	// 3) XML-style <function=name>{args}
	xmlMatches := xmlToolPattern.FindAllStringSubmatch(text, -1)
	for _, m := range xmlMatches {
		if len(m) >= 3 && availableTools[m[1]] {
			results = append(results, [2]string{m[1], m[2]})
		}
	}

	return results
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

// matchToolFuzzy intenta emparejar un nombre de herramienta con typos usando
// distancia de Levenshtein simple. Retorna el nombre correcto si la distancia
// es pequeña (≤3) o el nombre original si no hay match.
func matchToolFuzzy(requested string, available []domain.Tool) string {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested == "" {
		return requested
	}
	best := requested
	bestDist := -1
	for _, tool := range available {
		candidate := strings.ToLower(tool.Name)
		dist := levenshtein(requested, candidate)
		if bestDist == -1 || dist < bestDist {
			bestDist = dist
			best = tool.Name
		}
	}
	if bestDist >= 0 && bestDist <= 3 {
		return best
	}
	return requested
}

// levenshtein calcula la distancia de edición entre dos strings.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return len(b)
	}
	if b == "" {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	for i := range prev {
		prev[i] = i
	}
	for i, ca := range a {
		curr := make([]int, len(b)+1)
		curr[0] = i + 1
		for j, cb := range b {
			cost := 0
			if ca != cb {
				cost = 1
			}
			curr[j+1] = min(
				prev[j+1]+1,
				curr[j]+1,
				prev[j]+cost,
			)
		}
		prev = curr
	}
	return prev[len(b)]
}

// RecoverToolCallsFromText es el fallback público: extrae tool calls del texto
// libre del modelo, los parsea con tolerancia y corrige typos con fuzzy match.
// Devuelve llamadas listas para ejecutar. Vacío si no hay nada recuperable.
func RecoverToolCallsFromText(text string, tools []domain.Tool) []domain.ToolCall {
	if strings.TrimSpace(text) == "" || len(tools) == 0 {
		return nil
	}
	avail := make(map[string]bool, len(tools))
	for _, t := range tools {
		avail[t.Name] = true
	}
	pairs := extractToolCallsFromText(text, avail)
	// Si no hubo match estricto, intentar también fuzzy sobre nombres parecidos:
	// buscar cualquier {"name": "<x>", ...} y mapear con Levenshtein.
	if len(pairs) == 0 {
		fuzzyRe := regexp.MustCompile(`(?s)\{\s*"name"\s*:\s*"([^"]+)"`)
		for _, m := range fuzzyRe.FindAllStringSubmatch(text, -1) {
			if len(m) < 2 {
				continue
			}
			mapped := matchToolFuzzy(m[1], tools)
			if mapped == "" || mapped == strings.ToLower(strings.TrimSpace(m[1])) && !avail[m[1]] {
				continue
			}
			if !avail[mapped] {
				continue
			}
			// Extraer objeto JSON que contiene ese name como args candidatos.
			if obj := extractFirstJSON(m[0] + text[len(m[0]):]); obj != "" {
				if _, args := extractNameArgs(obj); args != "" {
					pairs = append(pairs, [2]string{mapped, args})
				}
			}
		}
	}
	var out []domain.ToolCall
	for i, p := range pairs {
		name := matchToolFuzzy(p[0], tools)
		args := map[string]any{}
		raw := p[1]
		if raw == "" || raw == "null" {
			raw = "{}"
		}
		if err := parseArgumentsTolerant(raw, args); err != nil {
			continue
		}
		out = append(out, domain.ToolCall{
			ID:        fmt.Sprintf("recovered_%d", i),
			Name:      name,
			Arguments: args,
		})
	}
	return out
}

func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
