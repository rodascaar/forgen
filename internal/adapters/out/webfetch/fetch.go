// Package webfetch implementa el fetch de páginas y la extracción de texto.
package webfetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// fetchTimeout limita la descarga de una página.
const fetchTimeout = 20 * time.Second

// maxBodyBytes limita el tamaño descargado.
const maxBodyBytes = 2 * 1024 * 1024

// maxTextChars limita el texto extraído.
const maxTextChars = 20000

// Fetch descarga una URL y devuelve su texto legible (HTML -> texto plano).
func Fetch(ctx context.Context, rawURL string) (string, error) {
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return "", fmt.Errorf("URL inválida %q (debe empezar con http:// o https://)", rawURL)
	}
	client := &http.Client{Timeout: fetchTimeout}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "forgen/0.1 (+https://github.com/forgen/forgen)")

	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch %s: http %d", rawURL, response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes))
	if err != nil {
		return "", err
	}

	text := ExtractText(string(body))
	if len(text) > maxTextChars {
		text = text[:maxTextChars] + "... [truncado]"
	}
	return text, nil
}

// extractText extrae el texto de un HTML omitiendo script/style/head.
func ExtractText(htmlContent string) string {
	reader := strings.NewReader(htmlContent)
	tokenizer := html.NewTokenizer(reader)

	var builder strings.Builder
	skipDepth := 0

	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			break
		}
		switch tokenType {
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := tokenizer.TagName()
			tag := strings.ToLower(string(name))
			if tag == "script" || tag == "style" || tag == "noscript" || tag == "head" {
				skipDepth++
			}
			if skipDepth == 0 && builder.Len() > 0 {
				// Separar bloques con espacio.
				if tokenType == html.StartTagToken && blockTag(tag) {
					builder.WriteString("\n")
				}
			}
		case html.EndTagToken:
			name, _ := tokenizer.TagName()
			tag := strings.ToLower(string(name))
			if tag == "script" || tag == "style" || tag == "noscript" || tag == "head" {
				if skipDepth > 0 {
					skipDepth--
				}
			}
			if skipDepth == 0 && blockTag(tag) {
				builder.WriteString("\n")
			}
		case html.TextToken:
			if skipDepth == 0 {
				text := strings.TrimSpace(string(tokenizer.Text()))
				if text != "" {
					if builder.Len() > 0 {
						builder.WriteString(" ")
					}
					builder.WriteString(text)
				}
			}
		}
	}

	return collapseWhitespace(builder.String())
}

func blockTag(tag string) bool {
	switch tag {
	case "p", "div", "h1", "h2", "h3", "h4", "h5", "h6", "li", "tr", "br", "section", "article", "pre":
		return true
	}
	return false
}

func collapseWhitespace(text string) string {
	lines := strings.Split(text, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return strings.Join(result, "\n")
}
