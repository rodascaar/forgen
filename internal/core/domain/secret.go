package domain

// MaskSecret enmascara un secreto para salida en logs, config, trace o errores.
// Solo muestra los últimos 4 caracteres si el secreto es largo; si es corto,
// devuelve un marcador genérico. Nunca revela el valor completo.
func MaskSecret(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 6 {
		return "****"
	}
	return "****" + secret[len(secret)-4:]
}
