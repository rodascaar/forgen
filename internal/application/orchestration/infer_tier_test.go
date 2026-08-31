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
		// Heurística de parámetros (agnóstica)
		{"my-model-7b", domain.TierLight},
		{"my-model-3b", domain.TierLight},
		{"my-model-9b", domain.TierLight},
		{"qwen2.5-7b-instruct", domain.TierLight},
		{"gemma-2b-it", domain.TierLight},
		{"phi-3.5-mini-4b", domain.TierLight},
		{"llama-3.2-3b", domain.TierLight},
		{"custom-12b", domain.TierStandard},
		{"model-30b", domain.TierStandard},
		{"model-70b", domain.TierHeavy},
		{"model-120b", domain.TierHeavy},
		{"model-405b", domain.TierHeavy},

		// Keywords legacy (compatibilidad)
		{"gpt-5-mini", domain.TierLight},
		{"gpt-5-nano", domain.TierLight},
		{"meta/llama-3.3-70b-instruct", domain.TierHeavy},
		{"nvidia/llama-3.1-nemotron-ultra-253b-v1", domain.TierHeavy},
		{"deepseek-r1", domain.TierHeavy},
		{"qwen-max", domain.TierHeavy},
		{"claude-sonnet-4-5", domain.TierStandard},
		{"gpt-5", domain.TierStandard},

		// Edge cases
		{"bloom-7b", domain.TierLight},       // tiene 7b aunque nombre raro
		{"babel-7b", domain.TierLight},       // match 7b
		{"no-params-here", domain.TierStandard}, // sin parámetros → standard
	}
	for _, tc := range cases {
		if got := inferTier(tc.id); got != tc.want {
			t.Errorf("inferTier(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}
