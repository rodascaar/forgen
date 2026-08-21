package search_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rodascaar/forgen/internal/adapters/out/search"
)

func TestBraveSearchParsesResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Subscription-Token") != "test-key" {
			t.Errorf("falta el header de suscripción")
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"web":{"results":[{"title":"Go","url":"https://go.dev","description":"El lenguaje"}]}}`))
	}))
	t.Cleanup(server.Close)

	// Inyectar la URL del servidor de prueba.
	provider := search.NewBraveSearchWithURL("test-key", server.URL)

	results, err := provider.Search(context.Background(), "golang", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].Title != "Go" || results[0].URL != "https://go.dev" {
		t.Fatalf("result = %+v", results[0])
	}
}

func TestBraveSearchMissingKey(t *testing.T) {
	provider := search.NewBraveSearch("")
	if _, err := provider.Search(context.Background(), "x", 5); err == nil {
		t.Fatal("esperaba error sin API key")
	}
}
