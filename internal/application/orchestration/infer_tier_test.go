package orchestration

import (
	"testing"

	"github.com/rodascaar/forgen/internal/core/domain"
)

func TestInferTier(t *testing.T) {
	cases := []struct {
		id   string
		want domain.Tier
	}{
		{"gpt-5-mini", domain.TierLight},
		{"gpt-5-nano", domain.TierLight},
		{"meta/llama-3.3-70b-instruct", domain.TierHeavy},
		{"nvidia/llama-3.1-nemotron-ultra-253b-v1", domain.TierHeavy},
		{"deepseek-r1", domain.TierHeavy},
		{"qwen-max", domain.TierHeavy},
		{"claude-sonnet-4-5", domain.TierStandard},
		{"gpt-5", domain.TierStandard},
	}
	for _, tc := range cases {
		if got := inferTier(tc.id); got != tc.want {
			t.Errorf("inferTier(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}
