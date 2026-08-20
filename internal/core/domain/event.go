package domain

import "time"

// EventType clasifica los eventos de dominio.
type EventType string

const (
	EventSessionCreated  EventType = "session.created"
	EventMessageAdded    EventType = "message.added"
	EventToolStarted     EventType = "tool.started"
	EventToolFinished    EventType = "tool.finished"
	EventPermissionGrant EventType = "permission.granted"
	EventPermissionDeny  EventType = "permission.denied"
	EventLLMRequest      EventType = "llm.request"
	EventLLMResponse     EventType = "llm.response"
	EventAgentFinished   EventType = "agent.finished"
)

// Event es un evento de dominio para auditabilidad y observabilidad.
type Event struct {
	Type      EventType
	SessionID string
	Data      map[string]any
	At        time.Time
}

// NewEvent construye un evento con timestamp.
func NewEvent(eventType EventType, sessionID string, data map[string]any) Event {
	return Event{
		Type:      eventType,
		SessionID: sessionID,
		Data:      data,
		At:        time.Now(),
	}
}
