// Package mcp implementa el cliente MCP sobre transporte stdio (JSON-RPC 2.0).
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/forgen/forgen/internal/core/ports"
)

// protocolVersion y clientName identifican al cliente en el handshake.
const (
	protocolVersion   = "2024-11-05"
	clientName        = "forgen"
	requestTimeout    = 30 * time.Second
	initializeTimeout = 10 * time.Second
)

// jsonrpcRequest es una petición JSON-RPC 2.0.
type jsonrpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// jsonrpcResponse es una respuesta JSON-RPC 2.0.
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// stdioClient implementa ports.MCPClient sobre un subproceso stdio.
type stdioClient struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	writer    *bufio.Writer
	reader    *bufio.Reader
	closeFunc func() error
	closeOnce sync.Once

	writeMu   sync.Mutex
	pendingMu sync.Mutex
	nextID    int64
	pending   map[int64]chan jsonrpcResponse
	closed    chan struct{}
}

// NewStdioClient lanza el servidor como subproceso y hace el handshake.
func NewStdioClient(command string, args []string, env map[string]string) (ports.MCPClient, error) {
	cmd := exec.Command(command, args...)
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp iniciar %q: %w", command, err)
	}

	client := newStdioClient(stdin, stdout, func() error {
		_ = cmd.Process.Kill()
		return cmd.Wait()
	})
	if err := client.initialize(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("mcp handshake con %q: %w", command, err)
	}
	return client, nil
}

// newStdioClient construye un cliente sobre streams (testable sin proceso).
func newStdioClient(stdin io.WriteCloser, stdout io.Reader, closeFunc func() error) *stdioClient {
	client := &stdioClient{
		stdin:     stdin,
		writer:    bufio.NewWriter(stdin),
		reader:    bufio.NewReader(stdout),
		pending:   make(map[int64]chan jsonrpcResponse),
		closed:    make(chan struct{}),
		closeFunc: closeFunc,
	}
	go client.readLoop()
	return client
}

// readLoop lee respuestas del servidor y las despacha por ID.
func (c *stdioClient) readLoop() {
	for {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			c.failAll(err)
			return
		}
		var response jsonrpcResponse
		if err := json.Unmarshal(line, &response); err != nil {
			continue // ignorar líneas no JSON (logs del servidor)
		}
		if response.ID == 0 {
			continue // notificación sin id
		}
		c.pendingMu.Lock()
		ch, ok := c.pending[response.ID]
		if ok {
			delete(c.pending, response.ID)
		}
		c.pendingMu.Unlock()
		if ok {
			ch <- response
		}
	}
}

func (c *stdioClient) failAll(err error) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	for id, ch := range c.pending {
		ch <- jsonrpcResponse{ID: id, Error: &jsonrpcError{Code: -32000, Message: err.Error()}}
		delete(c.pending, id)
	}
}

// call envía una petición y espera su respuesta por ID.
func (c *stdioClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.pendingMu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan jsonrpcResponse, 1)
	c.pending[id] = ch
	c.pendingMu.Unlock()

	request := jsonrpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, err := json.Marshal(request)
	if err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, err
	}

	c.writeMu.Lock()
	_, err = c.writer.Write(append(data, '\n'))
	if err == nil {
		err = c.writer.Flush()
	}
	c.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("mcp escribir %s: %w", method, err)
	}

	select {
	case response := <-ch:
		if response.Error != nil {
			return nil, fmt.Errorf("mcp %s: error %d: %s", method, response.Error.Code, response.Error.Message)
		}
		return response.Result, nil
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	}
}

// initialize realiza el handshake del protocolo MCP.
func (c *stdioClient) initialize() error {
	ctx, cancel := context.WithTimeout(context.Background(), initializeTimeout)
	defer cancel()
	params := map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": clientName, "version": "0.1.0"},
	}
	if _, err := c.call(ctx, "initialize", params); err != nil {
		return err
	}
	// Notificación initialized (sin id).
	notification := jsonrpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"}
	data, _ := json.Marshal(notification)
	c.writeMu.Lock()
	_, _ = c.writer.Write(append(data, '\n'))
	_ = c.writer.Flush()
	c.writeMu.Unlock()
	return nil
}

// ListTools implementa ports.MCPClient.
func (c *stdioClient) ListTools(ctx context.Context) ([]ports.MCPTool, error) {
	result, err := c.call(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return nil, fmt.Errorf("mcp tools/list: %w", err)
	}
	tools := make([]ports.MCPTool, 0, len(payload.Tools))
	for _, tool := range payload.Tools {
		tools = append(tools, ports.MCPTool{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
		})
	}
	return tools, nil
}

// CallTool implementa ports.MCPClient.
func (c *stdioClient) CallTool(ctx context.Context, name string, arguments map[string]any) (string, error) {
	result, err := c.call(ctx, "tools/call", map[string]any{"name": name, "arguments": arguments})
	if err != nil {
		return "", err
	}
	var payload struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &payload); err != nil {
		return "", fmt.Errorf("mcp tools/call %s: %w", name, err)
	}
	if payload.IsError {
		return "", fmt.Errorf("mcp tool %s devolvió error", name)
	}
	if len(payload.Content) == 0 {
		return "", nil
	}
	return payload.Content[0].Text, nil
}

// Close implementa ports.MCPClient.
func (c *stdioClient) Close() error {
	var err error
	c.closeOnce.Do(func() {
		select {
		case <-c.closed:
		default:
			close(c.closed)
		}
		err = c.closeFunc()
	})
	return err
}

// closeFunc cierra el proceso subyacente.
// (campo auxiliar; para streams puros puede ser no-op).
var _ ports.MCPClient = (*stdioClient)(nil)
