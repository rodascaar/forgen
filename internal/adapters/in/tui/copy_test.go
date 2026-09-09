package tui

import (
	"encoding/base64"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rodascaar/forgen/internal/core/domain"
)

func TestExtractCodeBlocks(t *testing.T) {
	text := "hola\n```sh\ndocker ps\n```\nmedio\n```python\nprint(1)\n```\nfin"
	blocks := extractCodeBlocks(text)
	if len(blocks) != 2 {
		t.Fatalf("bloques=%d, quiero 2", len(blocks))
	}
	if blocks[0].lang != "sh" || !strings.Contains(blocks[0].code, "docker ps") {
		t.Fatalf("bloque 0 inesperado: %+v", blocks[0])
	}
	if blocks[1].lang != "python" {
		t.Fatalf("bloque 1 lang=%q, quiero python", blocks[1].lang)
	}
	if len(extractCodeBlocks("sin bloques")) != 0 {
		t.Fatal("sin cercas debería dar 0 bloques")
	}
	// Cerca sin cierre se ignora.
	if len(extractCodeBlocks("```sh\nincompleto")) != 0 {
		t.Fatal("bloque sin cierre debería ignorarse")
	}
}

func TestStripPromptChars(t *testing.T) {
	got := stripPromptChars("$ docker ps\n❯ go test ./...")
	if got != "docker ps\ngo test ./..." {
		t.Fatalf("strip=%q", got)
	}
}

func TestOSC52Sequence(t *testing.T) {
	seq, err := osc52Sequence("hola", false)
	if err != nil {
		t.Fatal(err)
	}
	enc := strings.TrimPrefix(strings.TrimSuffix(seq, "\x07"), "\x1b]52;c;")
	decoded, err := base64.StdEncoding.DecodeString(enc)
	if err != nil || string(decoded) != "hola" {
		t.Fatalf("roundtrip falló: %q %v", decoded, err)
	}
	tmuxSeq, err := osc52Sequence("hola", true)
	if err != nil || !strings.HasPrefix(tmuxSeq, "\x1bPtmux;") {
		t.Fatalf("wrap tmux falló: %q %v", tmuxSeq, err)
	}
	if _, err := osc52Sequence(string(make([]byte, osc52MaxRawBytes+1)), false); err == nil {
		t.Fatal("payload gigante debería rechazarse")
	}
}

func TestCopyWithEnvSSHUsesOSC52(t *testing.T) {
	oscCalls := 0
	nativeCalls := 0
	method, err := copyWithEnv("hola", copyEnv{ssh: true},
		func(string) error { nativeCalls++; return nil },
		func(string) error { return nil },
		func(string) error { return nil },
		func(s string) error { oscCalls++; return nil })
	if err != nil || method != "OSC52" {
		t.Fatalf("method=%q err=%v", method, err)
	}
	if oscCalls != 1 || nativeCalls != 0 {
		t.Fatalf("SSH debe saltar nativo: osc=%d native=%d", oscCalls, nativeCalls)
	}
}

func TestCopyWithEnvLocalFallsBackToOSC52(t *testing.T) {
	oscCalls := 0
	method, err := copyWithEnv("hola", copyEnv{},
		func(string) error { return errForTest("sin xclip") },
		func(string) error { return nil },
		func(string) error { return nil },
		func(string) error { oscCalls++; return nil })
	if err != nil || method != "OSC52" {
		t.Fatalf("method=%q err=%v", method, err)
	}
	if oscCalls != 1 {
		t.Fatal("debería caer a OSC52")
	}
}

func errForTest(s string) error {
	return &testErr{s}
}

type testErr struct{ s string }

func (e *testErr) Error() string { return e.s }

func modelFromTea(t *testing.T, tm tea.Model) *Model {
	t.Helper()
	switch v := tm.(type) {
	case *Model:
		return v
	case Model:
		return &v
	default:
		t.Fatalf("modelo inesperado: %T", tm)
		return nil
	}
}

func TestCopyCodeListsBlocksWhenIndexInvalid(t *testing.T) {
	m := Model{styles: newStyles(domain.DefaultTheme())}
	m.transcript = []transcriptLine{{kind: "assistant", text: "```sh\na\n```\n```py\nb\n```"}}
	upd, _ := m.handleCopy([]string{"/copy", "code", "9"})
	mm := modelFromTea(t, upd)
	last := mm.transcript[len(mm.transcript)-1].text
	if !strings.Contains(last, "Hay 2 bloques") {
		t.Fatalf("debería listar bloques, got: %q", last)
	}
}

func TestCtrlYDoesNotToggleCollapse(t *testing.T) {
	m := Model{styles: newStyles(domain.DefaultTheme()), transcript: []transcriptLine{
		{kind: "assistant", text: "```sh\ndocker ps\n```"},
	}}
	upd, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlY})
	mm := modelFromTea(t, upd)
	if mm.transcript[0].collapsed {
		t.Fatal("Ctrl+Y no debe colapsar (eso es Ctrl+O)")
	}
	found := false
	for _, l := range mm.transcript {
		if strings.Contains(l.text, "Bloque") && strings.Contains(l.text, "copiado") {
			found = true
		}
	}
	if !found {
		t.Fatalf("Ctrl+Y debería yankear el bloque, transcript=%v", mm.transcript)
	}
}

func TestCtrlOCollapseStillWorks(t *testing.T) {
	m := Model{styles: newStyles(domain.DefaultTheme()), transcript: []transcriptLine{
		{kind: "assistant", text: "respuesta larga x"},
	}}
	upd, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlO})
	mm := modelFromTea(t, upd)
	if !mm.transcript[0].collapsed {
		t.Fatal("Ctrl+O debe seguir colapsando")
	}
}
