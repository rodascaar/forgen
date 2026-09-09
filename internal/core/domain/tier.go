package domain

import (
	"regexp"
	"strconv"
	"strings"
)

var tierParamRe = regexp.MustCompile(`(\d+(?:\.\d+)?)b\b`)

func InferTierFromID(id string) Tier {
	id = strings.ToLower(id)

	if matches := tierParamRe.FindStringSubmatch(id); len(matches) >= 2 {
		params, err := strconv.ParseFloat(matches[1], 64)
		if err == nil {
			switch {
			case params <= 9:
				return TierLight
			case params <= 16:
				return TierStandard
			default:
				// 24B/27B/30B/32B ya rinden como heavy en tool-calling y
				// necesitan presupuesto heavy (antes ≤30 caía a standard/1024).
				return TierHeavy
			}
		}
	}

	heavy := []string{"pro", "opus", "ultra", "max", "large", "405b", "253b", "120b", "70b", "30b", "32b", "27b", "24b", "qwen-max", "deepseek-r1", "nemotron-ultra", "gigant", "sonnet", "gemma-3-27", "qwen3-30", "qwen-30"}
	for _, kw := range heavy {
		if strings.Contains(id, kw) {
			return TierHeavy
		}
	}
	light := []string{"mini", "nano", "flash", "haiku", "small", "lite", "light", "fast"}
	for _, kw := range light {
		if strings.Contains(id, kw) {
			return TierLight
		}
	}
	return TierStandard
}
