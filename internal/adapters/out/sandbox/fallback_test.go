package sandbox

import (
	"context"

	"github.com/rodascaar/forgen/internal/core/ports"
)

// nilFallback es un executor que falla: solo para tests de degradación.
type nilFallback struct{}

func newNilFallback() ports.Executor { return nilFallback{} }

func (nilFallback) Execute(context.Context, string, string, []string) (ports.ExecResult, error) {
	return ports.ExecResult{}, context.Canceled
}
