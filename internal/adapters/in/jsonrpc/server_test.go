package jsonrpc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/forgen/forgen/internal/adapters/in/jsonrpc"
	apppkg "github.com/forgen/forgen/internal/app"
)

// buildApp crea un App apuntando a directorios temporales.
func buildApp(t *testing.T) *apppkg.App {
	t.Helper()
	t.Setenv("FORGEN_CONFIG_DIR", t.TempDir())
	t.Setenv("FORGEN_DATA_DIR", t.TempDir())
	app, err := apppkg.NewApp(slog.Default())
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	t.Cleanup(app.Close)
	return app
}

func TestPing(t *testing.T) {
	app := buildApp(t)
	input := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	var out bytes.Buffer
	server := jsonrpc.NewServer(app, strings.NewReader(input+"\n"), &out)

	if err := server.Serve(context.Background()); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var response map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &response); err != nil {
		t.Fatalf("respuesta inválida: %v", err)
	}
	if response["result"].(map[string]any)["pong"] != true {
		t.Fatalf("response = %v", response)
	}
}

func TestUnknownMethod(t *testing.T) {
	app := buildApp(t)
	input := `{"jsonrpc":"2.0","id":2,"method":"nope"}`
	var out bytes.Buffer
	server := jsonrpc.NewServer(app, strings.NewReader(input+"\n"), &out)

	if err := server.Serve(context.Background()); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var response map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out.String())), &response)
	if response["error"] == nil {
		t.Fatalf("esperaba error para método desconocido: %v", response)
	}
}

func TestAgentRunRejectsMissingPrompt(t *testing.T) {
	app := buildApp(t)
	input := `{"jsonrpc":"2.0","id":3,"method":"agent/run","params":{}}`
	var out bytes.Buffer
	server := jsonrpc.NewServer(app, strings.NewReader(input+"\n"), &out)

	if err := server.Serve(context.Background()); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var response map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSpace(out.String())), &response)
	if response["error"] == nil {
		t.Fatalf("esperaba error por prompt faltante: %v", response)
	}
}
