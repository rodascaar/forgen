package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGit es un git "real" que simula un workspace que NO es un repo git.
type fakeGit struct{}

func (f fakeGit) Status(ctx context.Context, workdir string) (string, error) {
	return "", errNotRepo
}

func (f fakeGit) Diff(ctx context.Context, workdir string, staged bool) (string, error) {
	return "", errNotRepo
}

func (f fakeGit) IsRepo(ctx context.Context, workdir string) (bool, error) {
	return false, nil
}

var errNotRepo = os.ErrNotExist

func TestCombinedGitFallsBackToInternal(t *testing.T) {
	root := t.TempDir()
	workdir := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatal(err)
	}

	combined := NewCombined(fakeGit{}, filepath.Join(root, "data"))

	// Workspace sin repo real: Status no debe fallar en rojo.
	status, err := combined.Status(context.Background(), workdir)
	if err != nil {
		t.Fatalf("Status no debería fallar sin repo real: %v", err)
	}
	if status != "" {
		t.Fatalf("esperaba status vacío en workspace limpio, obtuve %q", status)
	}

	// Añadir un archivo: el git interno debe reportarlo.
	file := filepath.Join(workdir, "a.txt")
	if err := os.WriteFile(file, []byte("hola"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err = combined.Status(context.Background(), workdir)
	if err != nil {
		t.Fatalf("Status falló: %v", err)
	}
	if !strings.Contains(status, "a.txt") {
		t.Fatalf("Status interno debería listar a.txt, obtuvo: %q", status)
	}

	// Diff interno debe describir los cambios.
	diff, err := combined.Diff(context.Background(), workdir, false)
	if err != nil {
		t.Fatalf("Diff falló: %v", err)
	}
	if !strings.Contains(diff, "a.txt") {
		t.Fatalf("Diff interno debería mencionar a.txt, obtuvo: %q", diff)
	}
}