package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rodascaar/forgen/internal/core/ports"
)

// fakeLSPServer responde al protocolo LSP sobre pipes.
type fakeLSPServer struct {
	reader *bufio.Reader
	writer *bufio.Writer
	mu     sync.Mutex
}

func (s *fakeLSPServer) serve(t *testing.T) {
	for {
		length, err := s.readFrameLen()
		if err != nil {
			return
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(s.reader, body); err != nil {
			return
		}
		var msg struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(body, &msg)

		if len(msg.ID) == 0 || string(msg.ID) == "null" {
			continue // notificación
		}
		var id int64
		_ = json.Unmarshal(msg.ID, &id)

		switch msg.Method {
		case "initialize":
			s.writeResponse(id, map[string]any{"capabilities": map[string]any{}})
		case "textDocument/hover":
			s.writeResponse(id, map[string]any{"contents": map[string]any{"kind": "markdown", "value": "docs de hover"}})
		case "textDocument/definition":
			s.writeResponse(id, map[string]any{
				"uri": "file:///x/a.go",
				"range": map[string]any{
					"start": map[string]any{"line": 2, "character": 4},
					"end":   map[string]any{"line": 2, "character": 8},
				},
			})
		case "textDocument/references":
			s.writeResponse(id, []map[string]any{
				{"uri": "file:///x/a.go", "range": map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 0, "character": 1}}},
			})
		case "textDocument/rename":
			s.writeResponse(id, map[string]any{
				"changes": map[string]any{
					"file:///x/a.go": []map[string]any{
						{"newText": "renamed", "range": map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 0, "character": 7}}},
					},
				},
			})
		default:
			s.writeResponse(id, nil)
		}
	}
}

func (s *fakeLSPServer) readFrameLen() (int, error) {
	length := -1
	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			return 0, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return length, nil
		}
		if after, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			length, _ = strconv.Atoi(strings.TrimSpace(after))
		}
	}
}

func (s *fakeLSPServer) writeResponse(id int64, result any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload := map[string]any{"jsonrpc": "2.0", "id": id, "result": result}
	data, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(s.writer, "Content-Length: %d\r\n\r\n%s", len(data), data)
	_ = s.writer.Flush()
}

// fakeFS lee/escribe en memoria para el cliente LSP.
type fakeFS struct {
	mu      sync.Mutex
	files   map[string]string
	written map[string]string
}

func (f *fakeFS) Read(_ context.Context, path string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []byte(f.files[path]), nil
}
func (f *fakeFS) Write(_ context.Context, path string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.written[path] = string(data)
	return nil
}
func (f *fakeFS) Exists(context.Context, string) (bool, error) { return true, nil }
func (f *fakeFS) Glob(context.Context, string) ([]string, error) {
	return nil, nil
}
func (f *fakeFS) Search(context.Context, string, string, string) ([]ports.SearchMatch, error) {
	return nil, nil
}

func newTestClient(t *testing.T) (*Client, *fakeFS) {
	t.Helper()
	serverReader, clientWriter := io.Pipe()
	clientReader, serverWriter := io.Pipe()

	fs := &fakeFS{files: map[string]string{"/x/a.go": "package x\nvar renamed = 1\n"}, written: map[string]string{}}
	server := &fakeLSPServer{reader: bufio.NewReader(serverReader), writer: bufio.NewWriter(serverWriter)}
	go server.serve(t)

	transport := newClient(clientWriter, clientReader, func() error { return nil })
	client := &Client{
		transport:  transport,
		fs:         fs,
		workspace:  "/x",
		languageID: "go",
		rootURI:    "file:///x",
		opened:     map[string]bool{},
	}
	if err := client.initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, fs
}

func TestLSPHoverAndDefinition(t *testing.T) {
	client, _ := newTestClient(t)
	ctx := context.Background()

	hover, err := client.Hover(ctx, "/x/a.go", 1, 1)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover != "docs de hover" {
		t.Fatalf("hover = %q", hover)
	}

	locations, err := client.Definition(ctx, "/x/a.go", 1, 1)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locations) != 1 || locations[0].Line != 3 || locations[0].Column != 5 {
		t.Fatalf("definition = %+v", locations)
	}
}

func TestLSPRenameAppliesEdit(t *testing.T) {
	client, fs := newTestClient(t)
	ctx := context.Background()

	if err := client.Rename(ctx, "/x/a.go", 1, 1, "renamed"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if fs.written["/x/a.go"] != "renamed x\nvar renamed = 1\n" {
		t.Fatalf("rename aplicado = %q", fs.written["/x/a.go"])
	}
}
