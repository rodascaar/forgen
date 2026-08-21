package tools_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rodascaar/forgen/internal/adapters/out/fs"
	"github.com/rodascaar/forgen/internal/application/tools"
	"github.com/rodascaar/forgen/internal/core/domain"
	"github.com/rodascaar/forgen/internal/core/ports"
)

func newRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	fileSystem := fs.New(t.TempDir())
	return tools.NewRegistry(fileSystem, nilExecutor{}, nilGit{}, 1000)
}

type nilExecutor struct{}

func (nilExecutor) Execute(context.Context, string, string, []string) (ports.ExecResult, error) {
	return ports.ExecResult{}, nil
}

type nilGit struct{}

func (nilGit) Status(context.Context, string) (string, error) { return "", nil }
func (nilGit) Diff(context.Context, string, bool) (string, error) {
	return "", nil
}
func (nilGit) IsRepo(context.Context, string) (bool, error) { return false, nil }

func TestWriteThenRead(t *testing.T) {
	registry := newRegistry(t)
	writeResult := registry.Execute(context.Background(), domain.ToolCall{
		ID:   "1",
		Name: "write",
		Arguments: map[string]any{
			"path":    "hello.go",
			"content": "package main\n",
		},
	})
	if !writeResult.OK {
		t.Fatalf("write falló: %v", writeResult.Error)
	}

	readResult := registry.Execute(context.Background(), domain.ToolCall{
		ID:        "2",
		Name:      "read",
		Arguments: map[string]any{"path": "hello.go"},
	})
	if !readResult.OK {
		t.Fatalf("read falló: %v", readResult.Error)
	}
	if readResult.Output != "package main" {
		t.Fatalf("read = %q", readResult.Output)
	}
}

func TestEditReplacesOnce(t *testing.T) {
	registry := newRegistry(t)
	registry.Execute(context.Background(), domain.ToolCall{
		ID:   "1",
		Name: "write",
		Arguments: map[string]any{
			"path":    "f.txt",
			"content": "a\nb\n",
		},
	})
	result := registry.Execute(context.Background(), domain.ToolCall{
		ID:   "2",
		Name: "edit",
		Arguments: map[string]any{
			"path":       "f.txt",
			"old_string": "b",
			"new_string": "c",
		},
	})
	if !result.OK {
		t.Fatalf("edit falló: %v", result.Error)
	}
	readResult := registry.Execute(context.Background(), domain.ToolCall{
		ID:        "3",
		Name:      "read",
		Arguments: map[string]any{"path": "f.txt"},
	})
	if !strings.Contains(readResult.Output, "c") {
		t.Fatalf("contenido = %q", readResult.Output)
	}
}

func TestEditAmbiguousOldStringFails(t *testing.T) {
	registry := newRegistry(t)
	registry.Execute(context.Background(), domain.ToolCall{
		ID:   "1",
		Name: "write",
		Arguments: map[string]any{
			"path":    "f.txt",
			"content": "x x\n",
		},
	})
	result := registry.Execute(context.Background(), domain.ToolCall{
		ID:   "2",
		Name: "edit",
		Arguments: map[string]any{
			"path":       "f.txt",
			"old_string": "x",
			"new_string": "y",
		},
	})
	if result.OK {
		t.Fatal("esperaba fallo por old_string ambiguo")
	}
}

func TestUnknownToolFails(t *testing.T) {
	registry := newRegistry(t)
	result := registry.Execute(context.Background(), domain.ToolCall{ID: "1", Name: "no_existe", Arguments: map[string]any{}})
	if result.OK {
		t.Fatal("esperaba fallo para herramienta desconocida")
	}
}

func TestListToolsContainsBuiltins(t *testing.T) {
	registry := newRegistry(t)
	tools := registry.ListTools()
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	for _, expected := range []string{"read", "write", "edit", "glob", "grep", "bash", "git_status", "git_diff"} {
		if !names[expected] {
			t.Fatalf("falta herramienta %q", expected)
		}
	}
}
