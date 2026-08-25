package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Service gestiona memoria persistente .forgen/memory.md y ~/.config/forgen/memory.md
type Service struct {
	workspace string
}

func New(workspace string) *Service { return &Service{workspace: workspace} }

func (s *Service) WorkspacePath() string { return filepath.Join(s.workspace, ".forgen", "memory.md") }

func (s *Service) LoadWorkspace(ctx context.Context) string {
	data, err := os.ReadFile(s.WorkspacePath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// AppendCompaction añade resumen de compaction a memoria workspace (ciclo compress→distill simple).
func (s *Service) AppendCompaction(summary string) {
	if strings.TrimSpace(summary) == "" {
		return
	}
	path := s.WorkspacePath()
	// G703: workspace validated via App.Workspace absolute, Clean guard
	clean := filepath.Clean(path)
	if !strings.HasPrefix(clean, filepath.Clean(s.workspace)) {
		return
	}
	path = clean
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	entry := "\n\n## compaction " + time.Now().Format("2006-01-02 15:04") + "\n" + strings.TrimSpace(summary) + "\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600) //nolint:gosec // G302 workspace file 0600
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.WriteString(entry)
	// cap file at ~20k chars (keep tail)
	if info, err := os.Stat(path); err == nil && info.Size() > 20000 {
		data, _ := os.ReadFile(path)
		if len(data) > 20000 {
			_ = os.WriteFile(path, data[len(data)-20000:], 0600) //nolint:gosec // G703 validated above
		}
	}
}
