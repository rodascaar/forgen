package storage_test

import (
	"context"
	"testing"

	"github.com/forgen/forgen/internal/adapters/out/storage"
	"github.com/forgen/forgen/internal/core/domain"
)

func TestJSONLExportImportRoundTrip(t *testing.T) {
	store, err := storage.NewJSONLStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONLStore: %v", err)
	}

	original := domain.Session{
		ID:        "export-me",
		Workspace: "/tmp",
		Model:     domain.Model{Provider: "openai", ID: "gpt-5"},
		Agent:     "build",
		Messages: []domain.Message{
			domain.NewTextMessage(domain.RoleUser, "hola"),
			domain.NewTextMessage(domain.RoleAssistant, "¿en qué ayudo?"),
		},
	}
	if err := store.Save(context.Background(), original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	exported, err := store.Export(context.Background(), "export-me")
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if len(exported) == 0 {
		t.Fatal("export vacío")
	}

	// Importar en un store nuevo (simula otra máquina).
	other, _ := storage.NewJSONLStore(t.TempDir())
	imported, err := other.Import(context.Background(), exported)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if imported.ID != "export-me" {
		t.Fatalf("ID importado = %q", imported.ID)
	}
	if len(imported.Messages) != 2 {
		t.Fatalf("mensajes = %d, want 2", len(imported.Messages))
	}
	if imported.Messages[0].Text() != "hola" {
		t.Fatalf("primer mensaje = %q", imported.Messages[0].Text())
	}
}
