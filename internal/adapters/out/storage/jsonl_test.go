package storage_test

import (
	"context"
	"testing"

	"github.com/rodascaar/forgen/internal/adapters/out/storage"
	"github.com/rodascaar/forgen/internal/core/domain"
)

func TestJSONLRoundTrip(t *testing.T) {
	store, err := storage.NewJSONLStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONLStore: %v", err)
	}

	session := domain.Session{
		ID:        "test-session",
		Workspace: "/tmp",
		Model:     domain.Model{Provider: "openai", ID: "gpt-5"},
		Agent:     "build",
		Messages: []domain.Message{
			domain.NewTextMessage(domain.RoleUser, "hola"),
			domain.NewAssistantWithToolCalls("", []domain.ToolCall{
				{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "main.go"}},
			}),
			domain.NewToolResultMessage("call-1", "read", domain.ToolResult{OK: true, Output: "package main"}),
		},
	}

	if err := store.Save(context.Background(), session); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := store.Load(context.Background(), "test-session")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.ID != session.ID {
		t.Fatalf("ID = %q", loaded.ID)
	}
	if loaded.Model.Key() != "openai/gpt-5" {
		t.Fatalf("Model = %q", loaded.Model.Key())
	}
	if len(loaded.Messages) != 3 {
		t.Fatalf("mensajes = %d, want 3", len(loaded.Messages))
	}

	// La llamada a herramienta con sus argumentos debe sobrevivir.
	calls := loaded.Messages[1].ToolCalls()
	if len(calls) != 1 || calls[0].Name != "read" || calls[0].Arguments["path"] != "main.go" {
		t.Fatalf("tool call no restaurada: %+v", calls)
	}
}

func TestJSONLListAndDelete(t *testing.T) {
	store, err := storage.NewJSONLStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONLStore: %v", err)
	}

	for _, id := range []string{"a", "b", "c"} {
		session := domain.Session{ID: id, Workspace: "/tmp", Model: domain.Model{Provider: "p", ID: "m"}}
		if err := store.Save(context.Background(), session); err != nil {
			t.Fatalf("Save %s: %v", id, err)
		}
	}

	listed, err := store.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("list = %d, want 3", len(listed))
	}

	if err := store.Delete(context.Background(), "b"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Load(context.Background(), "b"); err == nil {
		t.Fatal("esperaba error al cargar sesión borrada")
	}
}

func TestJSONLLoadMissing(t *testing.T) {
	store, err := storage.NewJSONLStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONLStore: %v", err)
	}
	if _, err := store.Load(context.Background(), "no-existe"); err == nil {
		t.Fatal("esperaba error al cargar sesión inexistente")
	}
}
