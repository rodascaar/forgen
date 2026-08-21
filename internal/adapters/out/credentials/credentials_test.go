package credentials_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rodascaar/forgen/internal/adapters/out/credentials"
)

func TestFileFallbackStore(t *testing.T) {
	// Forzar el backend de archivo para testear de forma determinista.
	t.Setenv("FORGEN_KEYRING", "file")

	dir := t.TempDir()
	path := filepath.Join(dir, "credentials")

	store := credentials.NewStore(path)
	ctx := context.Background()

	if err := store.Set(ctx, "providers/groq", "sk-secreto-1234"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := store.Get(ctx, "providers/groq")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "sk-secreto-1234" {
		t.Fatalf("valor inesperado: %q", got)
	}

	// Permisos del archivo deben ser restrictivos (0600). Los permisos POSIX
	// no se aplican en Windows, así que solo se comprueban en Unix.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("permisos esperados 0600, got %o", perm)
		}
	}

	if _, err := store.Get(ctx, "providers/noexiste"); err == nil {
		t.Fatal("esperaba error para clave inexistente")
	}

	if err := store.Delete(ctx, "providers/groq"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, "providers/groq"); err == nil {
		t.Fatal("esperaba error tras eliminar")
	}
}
