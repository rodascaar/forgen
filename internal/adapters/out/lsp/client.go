// Package lsp implementa el cliente del Language Server Protocol sobre stdio.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/forgen/forgen/internal/core/ports"
)

// requestTimeout limita las peticiones LSP.
const requestTimeout = 30 * time.Second

// diagnosticsTimeout es la espera para recibir publishDiagnostics.
const diagnosticsTimeout = 3 * time.Second

// clientImpl es el transporte JSON-RPC con framing Content-Length.
type clientImpl struct {
	writer    *bufio.Writer
	reader    *bufio.Reader
	closeFunc func() error
	closeOnce sync.Once

	writeMu   sync.Mutex
	pendingMu sync.Mutex
	nextID    int64
	pending   map[int64]chan json.RawMessage

	diagMu      sync.Mutex
	diagnostics map[string][]ports.LSPDiagnostic
	diagCh      map[string]chan struct{}
}

// NewStdioClient lanza el language server y hace el handshake initialize.
func NewStdioClient(command string, args []string) (*clientImpl, error) {
	cmd := exec.Command(command, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("lsp iniciar %q: %w", command, err)
	}

	client := newClient(stdin, stdout, func() error {
		_ = cmd.Process.Kill()
		return cmd.Wait()
	})
	return client, nil
}

// newClient construye el transporte sobre streams (testable sin proceso).
func newClient(stdin io.WriteCloser, stdout io.Reader, closeFunc func() error) *clientImpl {
	client := &clientImpl{
		writer:      bufio.NewWriter(stdin),
		reader:      bufio.NewReader(stdout),
		closeFunc:   closeFunc,
		pending:     make(map[int64]chan json.RawMessage),
		diagnostics: make(map[string][]ports.LSPDiagnostic),
		diagCh:      make(map[string]chan struct{}),
	}
	go client.readLoop()
	return client
}

// message es un mensaje JSON-RPC genérico (request/response/notificación).
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

// readLoop lee mensajes con framing Content-Length y los despacha.
func (c *clientImpl) readLoop() {
	for {
		length, err := c.readContentLength()
		if err != nil {
			return
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(c.reader, body); err != nil {
			return
		}
		var msg message
		if err := json.Unmarshal(body, &msg); err != nil {
			continue
		}

		// Notificación (sin id): publishDiagnostics u otras.
		if len(msg.ID) == 0 || string(msg.ID) == "null" {
			if msg.Method == "textDocument/publishDiagnostics" {
				c.handlePublishDiagnostics(msg.Params)
			}
			continue
		}

		var id int64
		if err := json.Unmarshal(msg.ID, &id); err != nil {
			continue
		}
		c.pendingMu.Lock()
		ch, ok := c.pending[id]
		if ok {
			delete(c.pending, id)
		}
		c.pendingMu.Unlock()
		if ok {
			ch <- msg.Result
		}
	}
}

// readContentLength lee los headers hasta la línea vacía y devuelve el tamaño.
func (c *clientImpl) readContentLength() (int, error) {
	length := -1
	for {
		line, err := c.reader.ReadString('\n')
		if err != nil {
			return 0, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			if length < 0 {
				return 0, fmt.Errorf("lsp: falta Content-Length header")
			}
			return length, nil
		}
		if strings.HasPrefix(line, "Content-Length:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return 0, fmt.Errorf("lsp: Content-Length inválido %q", value)
			}
			length = parsed
		}
	}
}

// call envía una petición y espera su resultado.
func (c *clientImpl) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.pendingMu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan json.RawMessage, 1)
	c.pending[id] = ch
	c.pendingMu.Unlock()

	payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	c.writeMu.Lock()
	err = c.writeFrame(data)
	c.writeMu.Unlock()
	if err != nil {
		return nil, err
	}

	select {
	case result := <-ch:
		return result, nil
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	}
}

// notify envía una notificación (sin id).
func (c *clientImpl) notify(method string, params any) error {
	payload := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.writeFrame(data)
}

// writeFrame escribe un mensaje con header Content-Length.
func (c *clientImpl) writeFrame(data []byte) error {
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))
	if _, err := c.writer.WriteString(header); err != nil {
		return err
	}
	if _, err := c.writer.Write(data); err != nil {
		return err
	}
	return c.writer.Flush()
}

// handlePublishDiagnostics actualiza el mapa de diagnósticos por URI.
func (c *clientImpl) handlePublishDiagnostics(raw json.RawMessage) {
	var params struct {
		URI         string `json:"uri"`
		Diagnostics []struct {
			Range struct {
				Start struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"start"`
			} `json:"range"`
			Severity int    `json:"severity"`
			Message  string `json:"message"`
			Source   string `json:"source"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return
	}

	diagnostics := make([]ports.LSPDiagnostic, 0, len(params.Diagnostics))
	for _, diag := range params.Diagnostics {
		diagnostics = append(diagnostics, ports.LSPDiagnostic{
			File:     uriToPath(params.URI),
			Line:     diag.Range.Start.Line + 1,
			Column:   diag.Range.Start.Character + 1,
			Severity: ports.LSPDiagnosticSeverity(diag.Severity),
			Message:  diag.Message,
		})
	}

	c.diagMu.Lock()
	c.diagnostics[params.URI] = diagnostics
	ch := c.diagCh[params.URI]
	c.diagMu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (c *clientImpl) waitForDiagnostics(ctx context.Context, uri string) []ports.LSPDiagnostic {
	c.diagMu.Lock()
	ch := c.diagCh[uri]
	if ch == nil {
		ch = make(chan struct{}, 1)
		c.diagCh[uri] = ch
	}
	c.diagMu.Unlock()

	select {
	case <-ch:
	case <-ctx.Done():
	case <-time.After(diagnosticsTimeout):
	}

	c.diagMu.Lock()
	defer c.diagMu.Unlock()
	return c.diagnostics[uri]
}

// uriToPath convierte un file:// URI a una ruta del sistema.
func uriToPath(uri string) string {
	return strings.TrimPrefix(uri, "file://")
}

// Close implementa el cierre del transporte.
func (c *clientImpl) Close() error {
	var err error
	c.closeOnce.Do(func() {
		_ = c.notify("shutdown", nil)
		_ = c.notify("exit", nil)
		err = c.closeFunc()
	})
	return err
}
