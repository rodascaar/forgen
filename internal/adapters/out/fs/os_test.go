package fs_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rodascaar/forgen/internal/adapters/out/fs"
)

func TestReadWriteRoundTrip(t *testing.T) {
	fileSystem := fs.New(t.TempDir())
	if err := fileSystem.Write(context.Background(), "dir/sub/file.txt", []byte("contenido")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	data, err := fileSystem.Read(context.Background(), "dir/sub/file.txt")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "contenido" {
		t.Fatalf("read = %q", string(data))
	}
}

func TestGlob(t *testing.T) {
	fileSystem := fs.New(t.TempDir())
	_ = fileSystem.Write(context.Background(), "a.go", []byte("package a"))
	_ = fileSystem.Write(context.Background(), "b.go", []byte("package b"))
	_ = fileSystem.Write(context.Background(), "c.txt", []byte("texto"))

	matches, err := fileSystem.Glob(context.Background(), "*.go")
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("glob = %v, want 2 archivos .go", matches)
	}
}

func TestReadOutsideWorkspaceFails(t *testing.T) {
	root := t.TempDir()
	fileSystem := fs.New(root)
	if _, err := fileSystem.Read(context.Background(), "../../etc/passwd"); err == nil {
		t.Fatal("read fuera del workspace debería fallar")
	}
	if _, err := fileSystem.Read(context.Background(), "/etc/passwd"); err == nil {
		t.Fatal("read absoluta fuera debería fallar")
	}
	// dentro funciona
	if _, err := fileSystem.Read(context.Background(), "a.go"); err == nil {
		t.Fatal("read de archivo inexistente dentro debería fallar con otro error (no encontró)")
	}
}

func TestGlobOutsideWorkspaceFails(t *testing.T) {
	fileSystem := fs.New(t.TempDir())
	if _, err := fileSystem.Glob(context.Background(), "../../*.go"); err == nil {
		t.Fatal("glob fuera del workspace debería fallar")
	}
}

func TestSearchFindsLines(t *testing.T) {
	fileSystem := fs.New(t.TempDir())
	_ = fileSystem.Write(context.Background(), "src/main.go", []byte("package main\nfunc main() {}\n"))

	matches, err := fileSystem.Search(context.Background(), ".", "func main", "*.go")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(matches))
	}
	if matches[0].Line != 2 {
		t.Fatalf("line = %d, want 2", matches[0].Line)
	}
}

func TestSearchRespectsInclude(t *testing.T) {
	fileSystem := fs.New(t.TempDir())
	_ = fileSystem.Write(context.Background(), "a.go", []byte("target"))
	_ = fileSystem.Write(context.Background(), "b.txt", []byte("target"))

	matches, err := fileSystem.Search(context.Background(), ".", "target", "*.txt")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(matches) != 1 || !strings.HasSuffix(matches[0].File, "b.txt") {
		t.Fatalf("matches = %v, want solo b.txt", matches)
	}
}

func TestSearchIncludeMatchesRootAndNested(t *testing.T) {
	// Cubre el bug de Windows: doublestar.Match usa "/" como separador
	// incluso cuando filepath.Separator es "\".
	fileSystem := fs.New(t.TempDir())
	_ = fileSystem.Write(context.Background(), "b.txt", []byte("target"))
	_ = fileSystem.Write(context.Background(), "src/c.txt", []byte("target"))
	_ = fileSystem.Write(context.Background(), "a.go", []byte("target"))

	matches, err := fileSystem.Search(context.Background(), ".", "target", "*.txt")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %d, want 2 (b.txt y src/c.txt)", len(matches))
	}
}

func TestSearchSkipsIgnoredDirs(t *testing.T) {
	fileSystem := fs.New(t.TempDir())
	_ = fileSystem.Write(context.Background(), "node_modules/dep.js", []byte("target"))
	_ = fileSystem.Write(context.Background(), "src/app.go", []byte("target"))

	matches, err := fileSystem.Search(context.Background(), ".", "target", "")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, match := range matches {
		if strings.Contains(match.File, "node_modules") {
			t.Fatalf("no debe indexar node_modules: %v", match.File)
		}
	}
}
