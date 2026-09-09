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

func TestIsOverflowTotalCountsSystemAndTools(t *testing.T) {
	m := domain.Model{Provider: "openai", ID: "gpt-5"}
	s := domain.Session{Messages: []domain.Message{
		domain.NewTextMessage(domain.RoleUser, "hi"),
	}}
	// Sin system/tools no hay overflow (legacy tampoco).
	if IsOverflowTotal(s, "", nil, m, nil, 0.85) {
		t.Fatalf("did not expect overflow for tiny session")
	}
	// System prompt gigante (~100k chars ≈ 25k tokens) + tools deben contar.
	// Con límite default 128k - 4k reserved = 124k usable, 85% ≈ 105k: 25k no basta,
	// pero con metadata de 32k (usable 28k, 85% ≈ 23.8k) sí debe dar overflow.
	md := map[string]domain.ModelMetadata{
		m.Key(): {ContextLimit: 32000, MaxOutput: 4000},
	}
	bigSystem := strings.Repeat("s", 100000)
	if !IsOverflowTotal(s, bigSystem, nil, m, md, 0.85) {
		t.Fatalf("expected overflow with big system prompt, total=%d", TotalTokens(s, bigSystem, nil))
	}
	// Y el legacy IsOverflow (solo mensajes) NO lo detecta: ese era el bug.
	if IsOverflow(s, m, md, 0.85) {
		t.Fatalf("legacy IsOverflow should not fire on system-only bloat")
	}
}

func TestUsageRatioLevels(t *testing.T) {
	m := domain.Model{Provider: "openai", ID: "gpt-5"}
	md := map[string]domain.ModelMetadata{
		m.Key(): {ContextLimit: 10000, MaxOutput: 1000}, // usable 9000
	}
	s := domain.Session{}
	if r := UsageRatio(s, "", nil, m, md); r != 0 {
		t.Fatalf("expected 0 ratio got %f", r)
	}
	// ~6500 tokens ≈ 72%: nivel aviso.
	s70 := domain.Session{Messages: []domain.Message{
		domain.NewTextMessage(domain.RoleUser, strings.Repeat("a", 26000)),
	}}
	r70 := UsageRatio(s70, "", nil, m, md)
	if r70 < WarnThreshold || r70 >= DefaultCompactionThreshold {
		t.Fatalf("expected warn-level ratio in [0.70,0.85) got %f", r70)
	}
}

func TestValidCompactionSummary(t *testing.T) {
	good := "## Objetivo\nx\n## Hecho\ny\n## Archivos\nz\n## Pendiente\nw\n" + strings.Repeat("texto ", 50)
	if !ValidCompactionSummary(good) {
		t.Fatalf("expected valid summary")
	}
	if ValidCompactionSummary("") {
		t.Fatalf("empty should be invalid")
	}
	if ValidCompactionSummary("resumen corto sin secciones") {
		t.Fatalf("short unstructured should be invalid")
	}
}

func TestApplyCompactionBoundary(t *testing.T) {
	s := domain.Session{Messages: make([]domain.Message, 35)}
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
	if len(vis) != 31 { // 1 summary + 30 tail
		t.Fatalf("expected 31 visible got %d", len(vis))
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
