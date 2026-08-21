package domain

import "testing"

func TestAppConfigUpsertProvider(t *testing.T) {
	config := DefaultAppConfig()
	initialCount := len(config.Providers)

	provider := ProviderConfig{
		Name:    "openai",
		Type:    ProviderTypeOpenAICompatible,
		BaseURL: "https://api.openai.com/v1",
		Models:  []string{"gpt-5", "gpt-5-mini"},
	}

	// Añadir un proveedor nuevo.
	updated := config.UpsertProvider(provider)
	if got, want := len(updated.Providers), initialCount; got != want {
		t.Fatalf("añadir nuevo proveedor: len=%d, quiero %d", got, want)
	}
	got, ok := updated.FindProvider("openai")
	if !ok {
		t.Fatal("el proveedor openai debería existir tras el upsert")
	}
	if len(got.Models) != 2 {
		t.Fatalf("los modelos del proveedor reemplazado deberían actualizarse, got %v", got.Models)
	}
}

func TestAppConfigUpsertProviderReplaces(t *testing.T) {
	config := DefaultAppConfig()
	provider := ProviderConfig{
		Name:    "openai",
		Type:    ProviderTypeOpenAICompatible,
		BaseURL: "https://api.openai.com/v1",
		Models:  []string{"reemplazado"},
	}
	updated := config.UpsertProvider(provider)
	got, ok := updated.FindProvider("openai")
	if !ok {
		t.Fatal("openai debería existir")
	}
	if len(updated.Providers) != len(config.Providers) {
		t.Fatalf("reemplazar no debe duplicar el proveedor: %d vs %d", len(updated.Providers), len(config.Providers))
	}
	if got.Models[0] != "reemplazado" {
		t.Fatalf("el proveedor debería haberse reemplazado, got %v", got.Models)
	}
}
