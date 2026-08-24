package domain

import "strings"

// ProviderPreset describe un proveedor de inferencia conocido. El usuario solo
// aporta su API key: el preset rellena base_url, tipo y variable de entorno por
// defecto. Los modelos se detectan consultando la API del proveedor.
type ProviderPreset struct {
	Name      string
	Type      ProviderType
	BaseURL   string
	APIKeyEnv string
	// Models son IDs conocidos por defecto (fallback si la API no responde).
	Models []string
}

// ProviderPresets devuelve el registro de proveedores soportados.
// Todas las base_url fueron verificadas contra la documentación oficial
// (agosto 2026). Cada proveedor expone listado vía GET {base}/models
// (OpenAI-compatible) salvo Anthropic (GET /v1/models nativo).
func ProviderPresets() []ProviderPreset {
	return []ProviderPreset{
		// Frontier
		{Name: "openai", Type: ProviderTypeOpenAICompatible, BaseURL: "https://api.openai.com/v1", APIKeyEnv: "OPENAI_API_KEY", Models: []string{"gpt-5"}},
		{Name: "anthropic", Type: ProviderTypeAnthropic, BaseURL: "https://api.anthropic.com", APIKeyEnv: "ANTHROPIC_API_KEY", Models: []string{"claude-sonnet-4-5"}},
		{Name: "gemini", Type: ProviderTypeOpenAICompatible, BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", APIKeyEnv: "GEMINI_API_KEY", Models: []string{"gemini-2.5-pro"}},
		{Name: "xai", Type: ProviderTypeOpenAICompatible, BaseURL: "https://api.x.ai/v1", APIKeyEnv: "XAI_API_KEY", Models: []string{"grok-4"}},
		{Name: "mistral", Type: ProviderTypeOpenAICompatible, BaseURL: "https://api.mistral.ai/v1", APIKeyEnv: "MISTRAL_API_KEY", Models: []string{"mistral-large-latest"}},
		{Name: "nvidia", Type: ProviderTypeOpenAICompatible, BaseURL: "https://integrate.api.nvidia.com/v1", APIKeyEnv: "NVIDIA_API_KEY", Models: []string{"nvidia/llama-3.1-nemotron-ultra-253b-v1", "meta/llama-3.3-70b-instruct"}},

		// Inferencia ultrarrápida
		{Name: "groq", Type: ProviderTypeOpenAICompatible, BaseURL: "https://api.groq.com/openai/v1", APIKeyEnv: "GROQ_API_KEY", Models: []string{"llama-3.3-70b-versatile"}},
		{Name: "cerebras", Type: ProviderTypeOpenAICompatible, BaseURL: "https://api.cerebras.ai/v1", APIKeyEnv: "CEREBRAS_API_KEY", Models: []string{"gpt-oss-120b"}},
		{Name: "sambanova", Type: ProviderTypeOpenAICompatible, BaseURL: "https://api.sambanova.ai/v1", APIKeyEnv: "SAMBANOVA_API_KEY", Models: []string{"Meta-Llama-3.3-70B-Instruct"}},
		{Name: "together", Type: ProviderTypeOpenAICompatible, BaseURL: "https://api.together.ai/v1", APIKeyEnv: "TOGETHER_API_KEY", Models: []string{"meta-llama/Llama-4-Maverick-17B-128E-Instruct-FP8"}},
		{Name: "fireworks", Type: ProviderTypeOpenAICompatible, BaseURL: "https://api.fireworks.ai/inference/v1", APIKeyEnv: "FIREWORKS_API_KEY", Models: []string{"accounts/fireworks/models/llama-v3p3-70b-instruct"}},

		// Reventa / agregador
		{Name: "openrouter", Type: ProviderTypeOpenAICompatible, BaseURL: "https://openrouter.ai/api/v1", APIKeyEnv: "OPENROUTER_API_KEY", Models: []string{"openai/gpt-5"}},

		// Chinos
		{Name: "deepseek", Type: ProviderTypeOpenAICompatible, BaseURL: "https://api.deepseek.com", APIKeyEnv: "DEEPSEEK_API_KEY", Models: []string{"deepseek-chat"}},
		{Name: "moonshot", Type: ProviderTypeOpenAICompatible, BaseURL: "https://api.moonshot.ai/v1", APIKeyEnv: "MOONSHOT_API_KEY", Models: []string{"kimi-k2"}},
		{Name: "zhipu", Type: ProviderTypeOpenAICompatible, BaseURL: "https://open.bigmodel.cn/api/paas/v4", APIKeyEnv: "ZAI_API_KEY", Models: []string{"glm-5"}},
		{Name: "minimax", Type: ProviderTypeOpenAICompatible, BaseURL: "https://api.minimax.io/v1", APIKeyEnv: "MINIMAX_API_KEY", Models: []string{"MiniMax-M3"}},
		// DashScope: el dominio internacional es dashscope-intl; el dominio
		// doméstico (China) es dashscope.aliyuncs.com.
		{Name: "qwen", Type: ProviderTypeOpenAICompatible, BaseURL: "https://dashscope-intl.aliyuncs.com/compatible-mode/v1", APIKeyEnv: "DASHSCOPE_API_KEY", Models: []string{"qwen-max"}},

		// Local
		{Name: "ollama", Type: ProviderTypeOpenAICompatible, BaseURL: "http://localhost:11434/v1", APIKeyEnv: "", Models: []string{"llama3"}},
	}
}

// FindProviderPreset localiza un preset por nombre (case-insensitive).
func FindProviderPreset(name string) (ProviderPreset, bool) {
	for _, preset := range ProviderPresets() {
		if strings.EqualFold(preset.Name, name) {
			return preset, true
		}
	}
	return ProviderPreset{}, false
}

// PresetNames devuelve los nombres de presets conocidos.
func PresetNames() []string {
	presets := ProviderPresets()
	names := make([]string, 0, len(presets))
	for _, preset := range presets {
		names = append(names, preset.Name)
	}
	return names
}

// ToProviderConfig convierte un preset en una ProviderConfig editable.
func (p ProviderPreset) ToProviderConfig() ProviderConfig {
	return ProviderConfig{
		Name:      p.Name,
		Type:      p.Type,
		BaseURL:   p.BaseURL,
		APIKeyEnv: p.APIKeyEnv,
		Models:    append([]string(nil), p.Models...),
	}
}
