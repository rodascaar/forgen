package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckpointStoreCreateAndRestore(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	store := NewCheckpointStore(filepath.Join(t.TempDir(), "checkpoints"))

	// Fichero inicial.
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	cp, err := store.Create(ctx, workspace, "s1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if cp.FileCount != 1 {
		t.Fatalf("FileCount = %d, quiero 1", cp.FileCount)
	}

	// Modificar el workspace como haría el agente.
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("MODIFICADO"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := store.Restore(ctx, cp.ID); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(workspace, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original" {
		t.Fatalf("tras restore el contenido es %q, quiero %q", data, "original")
	}
}

func TestCheckpointStoreListAndPrune(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	_ = os.WriteFile(filepath.Join(workspace, "a.txt"), []byte("x"), 0o644)
	store := NewCheckpointStore(filepath.Join(t.TempDir(), "checkpoints"))

	for i := range 3 {
		if _, err := store.Create(ctx, workspace, "s1"); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}
	list, err := store.List(ctx, "s1", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("List len = %d, quiero 3", len(list))
	}

	if err := store.Prune(ctx, 1); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	list, _ = store.List(ctx, "s1", 0)
	if len(list) != 1 {
		t.Fatalf("tras Prune(1) quedan %d, quiero 1", len(list))
	}
}
