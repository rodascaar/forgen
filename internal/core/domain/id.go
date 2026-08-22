package domain

import (
	"errors"
	"time"
)

// ErrNotFound se devuelve cuando un recurso no se encuentra.
var ErrNotFound = errors.New("no encontrado")

func generateID() string {
	return time.Now().Format("20060102150405.000000000") + "-" + randomString(8)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
	}
	return string(b)
}
