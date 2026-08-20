package domain

import "time"

// UsageRecord registra el consumo de tokens de una llamada al modelo.
type UsageRecord struct {
	SessionID    string    `json:"session_id"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	Phase        string    `json:"phase"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	At           time.Time `json:"at"`
}

// Key devuelve el identificador "provider/model".
func (r UsageRecord) Key() string { return r.Provider + "/" + r.Model }

// UsageSummary agrega el uso por modelo.
type UsageSummary struct {
	Model        string `json:"model"`
	Requests     int    `json:"requests"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
}
