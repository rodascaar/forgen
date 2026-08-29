package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound se devuelve cuando un recurso no se encuentra.
var ErrNotFound = errors.New("no encontrado")

func generateID() string {
	return time.Now().Format("20060102150405.000000000") + "-" + randomString(8)
}

func randomString(n int) string {
	// Usa uuid para entropía criptográfica; recorta a n caracteres hex sin guiones.
	id := uuid.NewString()
	var clean strings.Builder
	for _, ch := range id {
		if ch != '-' {
			clean.WriteString(string(ch))
		}
	}
	if len(clean.String()) >= n {
		return clean.String()[:n]
	}
	return clean.String()
}
