package storage

import (
	"context"
	"testing"

	"github.com/forgen/forgen/internal/core/domain"
)

func benchmarkSession() domain.Session {
	session := domain.Session{
		ID:        "bench-session",
		Workspace: "/tmp",
		Model:     domain.Model{Provider: "openai", ID: "gpt-5"},
		Agent:     "build",
	}
	for i := 0; i < 20; i++ {
		session.Messages = append(session.Messages,
			domain.NewTextMessage(domain.RoleUser, "explícame este código con detalle y contexto"),
			domain.NewAssistantWithToolCalls("voy a leer el archivo", []domain.ToolCall{
				{ID: "call-1", Name: "read", Arguments: map[string]any{"path": "main.go"}},
			}),
			domain.NewToolResultMessage("call-1", "read", domain.ToolResult{OK: true, Output: "package main\nfunc main() {}"}),
		)
	}
	return session
}

func BenchmarkJSONLSave(b *testing.B) {
	store, _ := NewJSONLStore(b.TempDir())
	session := benchmarkSession()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := store.Save(ctx, session); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJSONLLoad(b *testing.B) {
	store, _ := NewJSONLStore(b.TempDir())
	_ = store.Save(context.Background(), benchmarkSession())
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := store.Load(ctx, "bench-session"); err != nil {
			b.Fatal(err)
		}
	}
}
