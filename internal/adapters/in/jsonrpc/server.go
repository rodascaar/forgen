// Package jsonrpc implementa un servidor JSON-RPC 2.0 sobre stdio para
// integrar forgen con IDEs (protocolo estilo ACP).
package jsonrpc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	apppkg "github.com/rodascaar/forgen/internal/app"
	"github.com/rodascaar/forgen/internal/application/agent"
	"github.com/rodascaar/forgen/internal/core/domain"
)

// request es una petición JSON-RPC 2.0 entrante.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// Server procesa peticiones JSON-RPC línea a línea sobre stdio.
type Server struct {
	app     *apppkg.App
	in      io.Reader
	out     io.Writer
	writeMu sync.Mutex
}

// NewServer construye el servidor JSON-RPC.
func NewServer(app *apppkg.App, in io.Reader, out io.Writer) *Server {
	return &Server{app: app, in: in, out: out}
}

// Serve lee peticiones y responde hasta EOF.
func (s *Server) Serve(ctx context.Context) error {
	scanner := bufio.NewScanner(s.in)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		s.handle(ctx, req)
	}
	return scanner.Err()
}

func (s *Server) handle(ctx context.Context, req request) {
	switch req.Method {
	case "agent/run":
		s.handleRun(ctx, req)
	case "ping":
		s.respond(req.ID, map[string]any{"pong": true})
	case "agent/cancel":
		s.respond(req.ID, map[string]any{"cancelled": false})
	default:
		s.respondError(req.ID, -32601, fmt.Sprintf("método desconocido: %s", req.Method))
	}
}

// runParams son los argumentos de agent/run.
type runParams struct {
	Prompt    string `json:"prompt"`
	SessionID string `json:"session_id,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
}

func (s *Server) handleRun(ctx context.Context, req request) {
	var params runParams
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Prompt == "" {
		s.respondError(req.ID, -32602, "params inválidos: se requiere 'prompt'")
		return
	}

	messenger := newRPCMessenger(s, req.ID)
	appConfig, err := s.app.LoadConfig(ctx)
	if err != nil {
		s.respondError(req.ID, -32000, err.Error())
		return
	}

	model, provider, phase, err := s.app.ResolveRunModel(ctx, params.Prompt, params.Provider, params.Model)
	if err != nil {
		s.respondError(req.ID, -32000, err.Error())
		return
	}
	agentDef, err := s.app.SelectedAgent(appConfig, "")
	if err != nil {
		s.respondError(req.ID, -32000, err.Error())
		return
	}

	workspace, _ := os.Getwd()
	session := loadOrCreate(ctx, s.app, params.SessionID, workspace, model, agentDef.Name)

	runner, err := s.app.NewRunner(ctx, apppkg.RunnerDeps{
		Provider:  provider,
		Model:     model,
		Agent:     agentDef,
		Messenger: messenger,
		Responder: messenger,
		Workspace: workspace,
		SessionID: session.ID,
	})
	if err != nil {
		s.respondError(req.ID, -32000, err.Error())
		return
	}

	result, err := runner.Run(ctx, agent.RunInput{
		Session:    session,
		Agent:      agentDef,
		Workspace:  workspace,
		UserPrompt: params.Prompt,
		Phase:      phase,
	})
	if err != nil {
		s.respondError(req.ID, -32000, err.Error())
		return
	}
	s.respond(req.ID, map[string]any{
		"session_id": result.Session.ID,
		"text":       result.FinalText,
	})
}

func loadOrCreate(ctx context.Context, app *apppkg.App, sessionID, workspace string, model domain.Model, agentName string) domain.Session {
	if sessionID != "" {
		if session, err := app.SessionService.Resume(ctx, sessionID); err == nil {
			return session
		}
	}
	session, err := app.SessionService.Create(ctx, workspace, model, agentName)
	if err != nil {
		return domain.Session{ID: sessionID, Workspace: workspace, Model: model, Agent: agentName}
	}
	return session
}

// respond envía una respuesta JSON-RPC.
func (s *Server) respond(id json.RawMessage, result any) {
	s.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (s *Server) respondError(id json.RawMessage, code int, message string) {
	s.write(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
}

// notify envía una notificación (sin id).
func (s *Server) notify(method string, params any) {
	s.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (s *Server) write(payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, _ = s.out.Write(append(data, '\n'))
}

// rpcMessenger traduce los eventos del agente a notificaciones JSON-RPC.
type rpcMessenger struct {
	server *Server
	id     json.RawMessage
}

func newRPCMessenger(server *Server, id json.RawMessage) *rpcMessenger {
	return &rpcMessenger{server: server, id: id}
}

func (m *rpcMessenger) StreamText(_ string, delta string) {
	m.server.notify("agent/delta", map[string]any{"text": delta})
}
func (m *rpcMessenger) ToolStarted(_ string, call domain.ToolCall) {
	m.server.notify("agent/tool_started", map[string]any{"tool": call.Name, "arguments": call.Arguments})
}
func (m *rpcMessenger) ToolFinished(_ string, call domain.ToolCall, result domain.ToolResult) {
	m.server.notify("agent/tool_finished", map[string]any{"tool": call.Name, "ok": result.OK, "output": result.Output})
}
func (m *rpcMessenger) Notice(_ string, text string) {
	m.server.notify("agent/notice", map[string]any{"text": text})
}
func (m *rpcMessenger) Error(_ string, err error) {
	m.server.notify("agent/error", map[string]any{"error": err.Error()})
}
func (m *rpcMessenger) Finished(_ string, finalText string) {
	m.server.notify("agent/finished", map[string]any{"text": finalText})
}

// Confirm implementa ports.PermissionResponder (auto-deny en modo servidor).
func (m *rpcMessenger) Confirm(_ context.Context, _ string, call domain.ToolCall) (bool, error) {
	m.server.notify("agent/confirm", map[string]any{"tool": call.Name})
	return false, nil
}

// Remember implementa ports.PermissionResponder.
func (m *rpcMessenger) Remember(_ context.Context, _ string, _ domain.ToolCall, _ domain.PermissionLevel) error {
	return nil
}
