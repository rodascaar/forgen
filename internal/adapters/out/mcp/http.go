// Package mcp implementa el cliente MCP sobre HTTP/SSE (JSON-RPC 2.0).
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rodascaar/forgen/internal/core/ports"
)

// httpClient implementa ports.MCPClient sobre HTTP JSON-RPC.
type httpClient struct {
	baseURL    string
	headers    map[string]string
	httpClient *http.Client

	mu     sync.Mutex
	nextID int64
}

// NewHTTPClient crea un cliente MCP HTTP/SSE y hace handshake.
func NewHTTPClient(baseURL string, headers map[string]string) (ports.MCPClient, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("mcp http: url vacía")
	}
	client := &httpClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		headers:    headers,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	if err := client.initialize(); err != nil {
		return nil, fmt.Errorf("mcp handshake con %q: %w", baseURL, err)
	}
	return client, nil
}

func (c *httpClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	c.mu.Unlock()

	request := jsonrpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mcp http %s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("mcp http %s: status %d: %s", method, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		return c.readSSE(resp.Body, id)
	}

	var rpcResp jsonrpcResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &rpcResp); err != nil {
		return nil, fmt.Errorf("mcp http %s: parse: %w", method, err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("mcp %s: error %d: %s", method, rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

func (c *httpClient) readSSE(body io.Reader, expectedID int64) (json.RawMessage, error) {
	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		payload := line
		if strings.HasPrefix(line, "data:") {
			payload = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var rpcResp jsonrpcResponse
		if err := json.Unmarshal([]byte(payload), &rpcResp); err != nil {
			continue
		}
		if rpcResp.ID != expectedID && rpcResp.ID != 0 {
			continue
		}
		if rpcResp.Error != nil {
			return nil, fmt.Errorf("mcp sse: error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
		}
		if rpcResp.Result != nil {
			return rpcResp.Result, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("mcp sse: sin respuesta para id %d", expectedID)
}

func (c *httpClient) initialize() error {
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
	// Notificación initialized (best-effort).
	notification := jsonrpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"}
	data, _ := json.Marshal(notification)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	_, _ = c.httpClient.Do(req)
	return nil
}

// ListTools implementa ports.MCPClient.
func (c *httpClient) ListTools(ctx context.Context) ([]ports.MCPTool, error) {
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
	for _, t := range payload.Tools {
		tools = append(tools, ports.MCPTool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema})
	}
	return tools, nil
}

// CallTool implementa ports.MCPClient.
func (c *httpClient) CallTool(ctx context.Context, name string, arguments map[string]any) (string, error) {
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
func (c *httpClient) Close() error { return nil }

var _ ports.MCPClient = (*httpClient)(nil)
