package session

import (
	"strings"
	"testing"
	"time"

	"github.com/rodascaar/forgen/internal/core/domain"
)

func mkToolMsg(text string, toolName string) domain.Message {
	m := domain.NewToolResultMessage("id1", toolName, domain.ToolResult{OK: true, Output: text})
	m.ToolName = toolName
	return m
}

func TestIsOverflow(t *testing.T) {
	m := domain.Model{Provider: "openai", ID: "gpt-5"}
	// 500k chars ~125k tokens > 105k budget => overflow
	s := domain.Session{Messages: []domain.Message{
		domain.NewTextMessage(domain.RoleUser, strings.Repeat("a ", 250000)),
	}}
	if !IsOverflow(s, m, nil, 0.85) {
		t.Fatalf("expected overflow with large message tokens=%d", SessionTokens(s))
	}
	s2 := domain.Session{Messages: []domain.Message{
		domain.NewTextMessage(domain.RoleUser, "hi"),
	}}
	if IsOverflow(s2, m, nil, 0.85) {
		t.Fatalf("did not expect overflow for tiny session")
	}
}

func TestPruneProtectsRecent(t *testing.T) {
	// 6 tool msgs each ~50k chars (~12500 tokens) exceeds 40k protect window
	s := domain.Session{Messages: []domain.Message{
		domain.NewTextMessage(domain.RoleUser, "hola crea página"),
		mkToolMsg(strings.Repeat("x", 50000), "bash"),
		mkToolMsg(strings.Repeat("y", 50000), "bash"),
		mkToolMsg(strings.Repeat("z", 50000), "bash"),
		mkToolMsg(strings.Repeat("w", 50000), "bash"),
		domain.NewTextMessage(domain.RoleUser, "sigue con eso"),
		mkToolMsg(strings.Repeat("q", 50000), "bash"),
		mkToolMsg(strings.Repeat("r", 50000), "read"),
	}}
	pruned, n := Prune(s)
	_ = pruned
	// At least some pruned, but last tool outputs protected
	if n == 0 {
		t.Fatalf("expected some pruned")
	}
	// Last 40k should be protected -> last 4 tools protected? check CompactedAt nil for recent
	protectedCount := 0
	for i := len(pruned.Messages) - 4; i < len(pruned.Messages); i++ {
		if pruned.Messages[i].CompactedAt == nil {
			protectedCount++
		}
	}
	if protectedCount == 0 {
		t.Fatalf("recent should be protected")
	}
	now := time.Now()
	_ = now
}

func TestPruneNeverPrunesReadSkill(t *testing.T) {
	s := domain.Session{Messages: []domain.Message{
		domain.NewTextMessage(domain.RoleUser, "test"),
		mkToolMsg("skill content", "read_skill"),
		mkToolMsg(strings.Repeat("a", 50000), "bash"),
		domain.NewTextMessage(domain.RoleUser, "último"),
		mkToolMsg(strings.Repeat("b", 50000), "bash"),
	}}
	pruned, _ := Prune(s)
	for _, m := range pruned.Messages {
		if m.ToolName == "read_skill" && m.CompactedAt != nil {
			t.Fatalf("read_skill should never be pruned")
		}
	}
}

func TestVisibleMessagesPlaceholder(t *testing.T) {
	now := time.Now()
	s := domain.Session{Messages: []domain.Message{
		domain.NewTextMessage(domain.RoleUser, "hi"),
		{Role: domain.RoleTool, ToolName: "bash", ToolCallID: "1", Content: []domain.ContentPart{{Type: "text", Text: "big output"}}, CompactedAt: &now},
	}}
	vis := VisibleMessages(s)
	if vis[1].Text() != SummaryPlaceholder {
		t.Fatalf("expected placeholder got %q", vis[1].Text())
	}
}

func TestApplyCompactionBoundary(t *testing.T) {
	s := domain.Session{Messages: make([]domain.Message, 25)}
	for i := range s.Messages {
		s.Messages[i] = domain.NewTextMessage(domain.RoleUser, "msg")
	}
	s2 := ApplyCompaction(s, "resumen de prueba")
	if s2.CompactBoundary != 5 {
		t.Fatalf("expected boundary 5 got %d", s2.CompactBoundary)
	}
	if s2.CompactionSummary != "resumen de prueba" {
		t.Fatalf("summary mismatch")
	}
	vis := VisibleMessages(s2)
	if len(vis) != 21 { // 1 summary + 20 tail
		t.Fatalf("expected 21 visible got %d", len(vis))
	}
}

func TestDetectLanguage(t *testing.T) {
	es := domain.Session{Messages: []domain.Message{domain.NewTextMessage(domain.RoleUser, "añade una página")}}
	if DetectLanguage(es) != "es" {
		t.Fatalf("expected es")
	}
	en := domain.Session{Messages: []domain.Message{domain.NewTextMessage(domain.RoleUser, "add a page")}}
	if DetectLanguage(en) != "en" {
		t.Fatalf("expected en")
	}
}
