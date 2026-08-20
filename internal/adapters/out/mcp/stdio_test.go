package mcp

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

// fakeMCPServer simula un servidor MCP sobre streams en memoria.
type fakeMCPServer struct {
	decoder *json.Decoder
	writer  io.Writer
}

func (s *fakeMCPServer) serve() {
	for {
		var request struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := s.decoder.Decode(&request); err != nil {
			return
		}
		switch request.Method {
		case "initialize":
			s.respond(map[string]any{"id": request.ID, "result": map[string]any{
				"protocolVersion": protocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
			}})
		case "tools/list":
			s.respond(map[string]any{"id": request.ID, "result": map[string]any{
				"tools": []map[string]any{
					{"name": "search", "description": "busca", "inputSchema": map[string]any{"type": "object"}},
				},
			}})
		case "tools/call":
			var params struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(request.Params, &params)
			s.respond(map[string]any{"id": request.ID, "result": map[string]any{
				"content": []map[string]any{{"type": "text", "text": "resultado de " + params.Name}},
			}})
		default:
			if request.ID != 0 {
				s.respond(map[string]any{"id": request.ID, "result": map[string]any{}})
			}
		}
	}
}

func (s *fakeMCPServer) respond(value map[string]any) {
	data, _ := json.Marshal(value)
	_, _ = s.writer.Write(append(data, '\n'))
	if flusher, ok := s.writer.(interface{ Flush() error }); ok {
		_ = flusher.Flush()
	}
}

// newPipeClient crea un cliente stdio sobre pipes en memoria con un servidor fake.
func newPipeClient(t *testing.T) (*stdioClient, *fakeMCPServer) {
	t.Helper()
	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()

	server := &fakeMCPServer{decoder: json.NewDecoder(serverReader), writer: serverWriter}
	go server.serve()

	client := newStdioClient(clientWriter, clientReader, func() error {
		_ = clientWriter.Close()
		return nil
	})
	if err := client.initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, server
}

func TestMCPListAndCallTool(t *testing.T) {
	client, _ := newPipeClient(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "search" {
		t.Fatalf("tools = %+v", tools)
	}

	output, err := client.CallTool(ctx, "search", map[string]any{"query": "x"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if output != "resultado de search" {
		t.Fatalf("output = %q", output)
	}
}

func TestMCPToolErrorPropagates(t *testing.T) {
	// Servidor que devuelve isError=true.
	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()

	server := &fakeMCPServer{decoder: json.NewDecoder(serverReader), writer: serverWriter}
	go func() {
		for {
			var request struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
			}
			if err := json.NewDecoder(serverReader).Decode(&request); err != nil {
				return
			}
			switch request.Method {
			case "initialize":
				server.respond(map[string]any{"id": request.ID, "result": map[string]any{"protocolVersion": protocolVersion}})
			case "tools/call":
				server.respond(map[string]any{"id": request.ID, "result": map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": "boom"}}}})
			}
		}
	}()

	client := newStdioClient(clientWriter, clientReader, func() error { return nil })
	if err := client.initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.CallTool(ctx, "broken", nil); err == nil {
		t.Fatal("esperaba error para tool con isError=true")
	}
}
