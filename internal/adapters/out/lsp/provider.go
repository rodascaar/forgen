package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rodascaar/forgen/internal/core/ports"
)

// Client implementa ports.LSPClient sobre el transporte stdio.
type Client struct {
	transport  *clientImpl
	fs         ports.FileSystem
	workspace  string
	languageID string
	rootURI    string
	version    int
	opened     map[string]bool
}

// NewClient lanza el language server y hace el handshake initialize.
func NewClient(command string, args []string, fs ports.FileSystem, workspace, languageID string) (*Client, error) {
	transport, err := NewStdioClient(command, args)
	if err != nil {
		return nil, err
	}
	client := &Client{
		transport:  transport,
		fs:         fs,
		workspace:  workspace,
		languageID: languageID,
		rootURI:    pathToURI(workspace),
		opened:     map[string]bool{},
	}
	if err := client.initialize(); err != nil {
		_ = transport.Close()
		return nil, fmt.Errorf("lsp initialize: %w", err)
	}
	return client, nil
}

func (c *Client) initialize() error {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	params := map[string]any{
		"processId": nil,
		"rootUri":   c.rootURI,
		"capabilities": map[string]any{
			"textDocument": map[string]any{},
		},
		"workspaceFolders": []map[string]any{{"uri": c.rootURI, "name": "workspace"}},
	}
	if _, err := c.transport.call(ctx, "initialize", params); err != nil {
		return err
	}
	_ = c.transport.notify("initialized", map[string]any{})
	return nil
}

// openDocument envía didOpen si el documento no está abierto.
func (c *Client) openDocument(ctx context.Context, path string) error {
	uri := pathToURI(path)
	if c.opened[uri] {
		return nil
	}
	content, err := c.readContent(ctx, path)
	if err != nil {
		return err
	}
	c.version++
	params := map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": c.languageID,
			"version":    c.version,
			"text":       content,
		},
	}
	if err := c.transport.notify("textDocument/didOpen", params); err != nil {
		return err
	}
	c.opened[uri] = true
	return nil
}

func (c *Client) readContent(ctx context.Context, path string) (string, error) {
	data, err := c.fs.Read(ctx, path)
	if err != nil {
		return "", fmt.Errorf("leer %s para lsp: %w", path, err)
	}
	return string(data), nil
}

// SyncDocument notifica al language server que un documento abierto cambió.
func (c *Client) SyncDocument(ctx context.Context, path string) error {
	uri := pathToURI(path)
	if !c.opened[uri] {
		return nil // no abierto: no requiere sincronización
	}
	content, err := c.readContent(ctx, path)
	if err != nil {
		return err
	}
	c.version++
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": c.version},
		"contentChanges": []map[string]any{
			{"text": content},
		},
	}
	return c.transport.notify("textDocument/didChange", params)
}

// Diagnostics implementa ports.LSPClient.
func (c *Client) Diagnostics(ctx context.Context, path string) ([]ports.LSPDiagnostic, error) {
	if err := c.openDocument(ctx, path); err != nil {
		return nil, err
	}
	return c.transport.waitForDiagnostics(ctx, pathToURI(path)), nil
}

// Hover implementa ports.LSPClient.
func (c *Client) Hover(ctx context.Context, path string, line, column int) (string, error) {
	if err := c.openDocument(ctx, path); err != nil {
		return "", err
	}
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"position":     positionParams(line, column),
	}
	result, err := c.transport.call(ctx, "textDocument/hover", params)
	if err != nil {
		return "", err
	}
	return parseHover(result), nil
}

// Definition implementa ports.LSPClient.
func (c *Client) Definition(ctx context.Context, path string, line, column int) ([]ports.LSPLocation, error) {
	if err := c.openDocument(ctx, path); err != nil {
		return nil, err
	}
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"position":     positionParams(line, column),
	}
	result, err := c.transport.call(ctx, "textDocument/definition", params)
	if err != nil {
		return nil, err
	}
	return parseLocations(result), nil
}

// References implementa ports.LSPClient.
func (c *Client) References(ctx context.Context, path string, line, column int) ([]ports.LSPLocation, error) {
	if err := c.openDocument(ctx, path); err != nil {
		return nil, err
	}
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"position":     positionParams(line, column),
		"context":      map[string]any{"includeDeclaration": true},
	}
	result, err := c.transport.call(ctx, "textDocument/references", params)
	if err != nil {
		return nil, err
	}
	return parseLocations(result), nil
}

// Rename implementa ports.LSPClient.
func (c *Client) Rename(ctx context.Context, path string, line, column int, newName string) error {
	if err := c.openDocument(ctx, path); err != nil {
		return err
	}
	params := map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"position":     positionParams(line, column),
		"newName":      newName,
	}
	result, err := c.transport.call(ctx, "textDocument/rename", params)
	if err != nil {
		return err
	}
	return c.applyWorkspaceEdit(result)
}

// applyWorkspaceEdit aplica el map changes (uri -> textEdits) al filesystem.
func (c *Client) applyWorkspaceEdit(result json.RawMessage) error {
	var edit struct {
		Changes map[string][]struct {
			NewText string `json:"newText"`
			Range   struct {
				Start struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"start"`
				End struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"end"`
			} `json:"range"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(result, &edit); err != nil {
		return fmt.Errorf("lsp rename: %w", err)
	}
	if len(edit.Changes) == 0 {
		return fmt.Errorf("lsp rename: sin cambios")
	}
	for uri, edits := range edit.Changes {
		path := uriToPath(uri)
		content, err := c.readContent(context.Background(), path)
		if err != nil {
			return err
		}
		lines := strings.Split(content, "\n")
		// Aplicar ediciones de abajo hacia arriba para no desincronizar offsets.
		for i := len(edits) - 1; i >= 0; i-- {
			change := edits[i]
			lines = applyTextEdit(lines, change.Range.Start.Line, change.Range.Start.Character,
				change.Range.End.Line, change.Range.End.Character, change.NewText)
		}
		if err := c.fs.Write(context.Background(), path, []byte(strings.Join(lines, "\n"))); err != nil {
			return err
		}
	}
	return nil
}

func applyTextEdit(lines []string, startLine, startChar, endLine, endChar int, newText string) []string {
	if startLine == endLine {
		line := lines[startLine]
		lines[startLine] = line[:startChar] + newText + line[endChar:]
		return lines
	}
	// Rango multilínea.
	first := lines[startLine][:startChar]
	last := lines[endLine][endChar:]
	result := append([]string{first + newText + last}, lines[endLine+1:]...)
	return append(lines[:startLine], result...)
}

// Close implementa ports.LSPClient.
func (c *Client) Close() error { return c.transport.Close() }

func positionParams(line, column int) map[string]any {
	return map[string]any{"line": line - 1, "character": column - 1}
}

func pathToURI(path string) string {
	return "file://" + path
}

func parseHover(result json.RawMessage) string {
	var hover struct {
		Contents any `json:"contents"`
	}
	if err := json.Unmarshal(result, &hover); err != nil || hover.Contents == nil {
		return ""
	}
	switch contents := hover.Contents.(type) {
	case string:
		return contents
	case map[string]any:
		if value, ok := contents["value"].(string); ok {
			return value
		}
	}
	return ""
}

func parseLocations(result json.RawMessage) []ports.LSPLocation {
	if len(result) == 0 || string(result) == "null" {
		return nil
	}

	// Puede ser un solo Location o un array.
	var locations []ports.LSPLocation
	if string(result[0]) == "[" {
		var raw []jsonLocation
		if err := json.Unmarshal(result, &raw); err != nil {
			return nil
		}
		for _, loc := range raw {
			locations = append(locations, loc.toPort())
		}
		return locations
	}
	var single jsonLocation
	if err := json.Unmarshal(result, &single); err != nil {
		return nil
	}
	if single.URI == "" {
		return nil
	}
	return []ports.LSPLocation{single.toPort()}
}

type jsonLocation struct {
	URI   string `json:"uri"`
	Range struct {
		Start struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"start"`
		End struct {
			Line      int `json:"line"`
			Character int `json:"character"`
		} `json:"end"`
	} `json:"range"`
}

func (l jsonLocation) toPort() ports.LSPLocation {
	return ports.LSPLocation{
		File:      uriToPath(l.URI),
		Line:      l.Range.Start.Line + 1,
		Column:    l.Range.Start.Character + 1,
		EndLine:   l.Range.End.Line + 1,
		EndColumn: l.Range.End.Character + 1,
	}
}

var _ ports.LSPClient = (*Client)(nil)
