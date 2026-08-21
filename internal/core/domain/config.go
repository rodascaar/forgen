package domain

import "fmt"

// ProviderType identifica el protocolo de un proveedor LLM.
type ProviderType string

const (
	ProviderTypeOpenAICompatible ProviderType = "openai_compatible"
	ProviderTypeAnthropic        ProviderType = "anthropic"
	ProviderTypeKimchi           ProviderType = "kimchi"
)

// ProviderConfig describe un proveedor configurado por el usuario.
type ProviderConfig struct {
	Name      string       `yaml:"name"`
	Type      ProviderType `yaml:"type"`
	BaseURL   string       `yaml:"base_url,omitempty"`
	APIKeyEnv string       `yaml:"api_key_env,omitempty"`
	Models    []string     `yaml:"models"`
}

// ResolveAPIKey devuelve la key de la variable de entorno o cadena vacía.
func (p ProviderConfig) ResolveAPIKey(getenv func(string) string) string {
	if p.APIKeyEnv == "" {
		return ""
	}
	return getenv(p.APIKeyEnv)
}

// DefaultSelection define el proveedor y modelo por defecto.
type DefaultSelection struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

// MCPServerConfig describe un servidor MCP a lanzar.
type MCPServerConfig struct {
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
}

// SearchConfig configura el proveedor de búsqueda web.
type SearchConfig struct {
	Provider  string `yaml:"provider"` // brave | "" (deshabilitado)
	APIKeyEnv string `yaml:"api_key_env"`
}

// Theme define la paleta de colores de la interfaz.
type Theme struct {
	User      string `yaml:"user"`
	Assistant string `yaml:"assistant"`
	Tool      string `yaml:"tool"`
	ToolDone  string `yaml:"tool_done"`
	Notice    string `yaml:"notice"`
	Error     string `yaml:"error"`
	Accent    string `yaml:"accent"`
	Dim       string `yaml:"dim"`
}

// DefaultTheme devuelve la paleta por defecto (Tokyo Night).
func DefaultTheme() Theme {
	return Theme{
		User:      "#7aa2f7",
		Assistant: "#c0caf5",
		Tool:      "#2ac3de",
		ToolDone:  "#9ece6a",
		Notice:    "#e0af68",
		Error:     "#f7768e",
		Accent:    "#bb9af7",
		Dim:       "#565f89",
	}
}

// ExecutionConfig configura el modo de ejecución de comandos.
type ExecutionConfig struct {
	Sandbox     string `yaml:"sandbox"` // "" | docker
	DockerImage string `yaml:"docker_image"`
}

// PermissionConfig es la configuración global de permisos.
type PermissionConfig struct {
	Mode  string           `yaml:"mode"` // auto | on_request | never
	Rules []PermissionRule `yaml:"rules,omitempty"`
}

// AppConfig es la configuración completa de la aplicación.
type AppConfig struct {
	Providers      []ProviderConfig           `yaml:"providers"`
	Default        DefaultSelection           `yaml:"default"`
	Permissions    PermissionConfig           `yaml:"permissions"`
	Agent          string                     `yaml:"agent"`
	MaxIterations  int                        `yaml:"max_iterations"`
	MaxOutputChars int                        `yaml:"max_output_chars"`
	ModelRoles     map[string][]string        `yaml:"model_roles,omitempty"`
	ModelMetadata  map[string]ModelMetadata   `yaml:"model_metadata,omitempty"`
	MCPServers     map[string]MCPServerConfig `yaml:"mcp_servers,omitempty"`
	Search         SearchConfig               `yaml:"search,omitempty"`
	Theme          Theme                      `yaml:"theme,omitempty"`
	Execution      ExecutionConfig            `yaml:"execution,omitempty"`
}

// DefaultAppConfig devuelve una configuración por defecto usable.
func DefaultAppConfig() AppConfig {
	return AppConfig{
		Providers: []ProviderConfig{
			{
				Name:      "openai",
				Type:      ProviderTypeOpenAICompatible,
				BaseURL:   "https://api.openai.com/v1",
				APIKeyEnv: "OPENAI_API_KEY",
				Models:    []string{"gpt-5"},
			},
			{
				Name:      "anthropic",
				Type:      ProviderTypeAnthropic,
				BaseURL:   "https://api.anthropic.com",
				APIKeyEnv: "ANTHROPIC_API_KEY",
				Models:    []string{"claude-sonnet-4-5"},
			},
		},
		Default:        DefaultSelection{Provider: "openai", Model: "gpt-5"},
		Permissions:    PermissionConfig{Mode: "auto"},
		Agent:          "build",
		MaxIterations:  50,
		MaxOutputChars: 30000,
		Theme:          DefaultTheme(),
	}
}

// UpsertProvider añade o reemplaza un proveedor y devuelve la config resultante.
func (c AppConfig) UpsertProvider(provider ProviderConfig) AppConfig {
	for i := range c.Providers {
		if c.Providers[i].Name == provider.Name {
			c.Providers[i] = provider
			return c
		}
	}
	c.Providers = append(c.Providers, provider)
	return c
}

// FindProvider localiza un proveedor por nombre.
func (c AppConfig) FindProvider(name string) (ProviderConfig, bool) {
	for _, provider := range c.Providers {
		if provider.Name == name {
			return provider, true
		}
	}
	return ProviderConfig{}, false
}

// Validate verifica la consistencia de la configuración.
// El listado de modelos es informativo (se detecta vía 'forgen auth'), por lo
// que no se exige que un proveedor defina modelos para considerarse válido.
func (c AppConfig) Validate() error {
	if _, ok := c.FindProvider(c.Default.Provider); !ok {
		return fmt.Errorf("proveedor por defecto %q no está configurado", c.Default.Provider)
	}
	return nil
}
