package cli

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	apppkg "github.com/forgen/forgen/internal/app"
)

func TestTraceRedactsSecrets(t *testing.T) {
	const secret = "sk-supersecret-value"

	t.Setenv("FORGEN_CONFIG_DIR", t.TempDir())
	t.Setenv("FORGEN_DATA_DIR", t.TempDir())
	t.Setenv("OPENAI_API_KEY", secret)

	app, err := apppkg.NewApp(slog.Default())
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	defer app.Close()

	report := buildTrace(context.Background(), app)

	if strings.Contains(report, secret) {
		t.Fatal("el trace no debe contener la API key")
	}
	if !strings.Contains(report, "OPENAI_API_KEY") {
		t.Fatal("el trace debe indicar qué variables están definidas")
	}
	if !strings.Contains(report, "definida") {
		t.Fatal("el trace debe indicar que la key está definida (sin revelarla)")
	}
}
