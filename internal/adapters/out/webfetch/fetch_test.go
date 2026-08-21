package webfetch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rodascaar/forgen/internal/adapters/out/webfetch"
)

func TestFetchStripsHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/html")
		_, _ = writer.Write([]byte(`<html><head><style>body{}</style></head><body><h1>Título</h1><p>Contenido <b>importante</b>.</p><script>var x=1;</script></body></html>`))
	}))
	t.Cleanup(server.Close)

	text, err := webfetch.Fetch(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	for _, want := range []string{"Título", "Contenido", "importante"} {
		if !contains(text, want) {
			t.Fatalf("text %q no contiene %q", text, want)
		}
	}
	if contains(text, "body{}") || contains(text, "var x") {
		t.Fatalf("no debe incluir style/script: %q", text)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestFetchRejectsInvalidURL(t *testing.T) {
	if _, err := webfetch.Fetch(context.Background(), "ftp://example.com"); err == nil {
		t.Fatal("esperaba error para esquema no http")
	}
}

func TestFetchHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	if _, err := webfetch.Fetch(context.Background(), server.URL); err == nil {
		t.Fatal("esperaba error para 404")
	}
}

func TestExtractTextSkipScript(t *testing.T) {
	html := `<html><body><p>visible</p><script>secreto()</script></body></html>`
	text := webfetch.ExtractText(html)
	if text != "visible" {
		t.Fatalf("text = %q", text)
	}
}
