// Package search implementa adapters de búsqueda web.
package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/forgen/forgen/internal/core/ports"
)

// braveSearchURL es el endpoint de la API de Brave Search.
const braveSearchURL = "https://api.search.brave.com/res/v1/web/search"

// braveClientTimeout limita cada petición de búsqueda.
const braveClientTimeout = 20 * time.Second

// BraveSearch implementa ports.SearchProvider con la API de Brave.
type BraveSearch struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewBraveSearch construye el adapter de Brave con el endpoint por defecto.
func NewBraveSearch(apiKey string) *BraveSearch {
	return NewBraveSearchWithURL(apiKey, braveSearchURL)
}

// NewBraveSearchWithURL construye el adapter con un endpoint personalizado.
func NewBraveSearchWithURL(apiKey, baseURL string) *BraveSearch {
	return &BraveSearch{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{Timeout: braveClientTimeout},
	}
}

// Search implementa ports.SearchProvider.
func (b *BraveSearch) Search(ctx context.Context, query string, limit int) ([]ports.SearchResult, error) {
	if b.apiKey == "" {
		return nil, fmt.Errorf("brave search: falta API key (configura search.api_key_env)")
	}
	if limit <= 0 || limit > 20 {
		limit = 10
	}

	endpoint, err := url.Parse(b.baseURL)
	if err != nil {
		return nil, err
	}
	params := endpoint.Query()
	params.Set("q", query)
	params.Set("count", fmt.Sprintf("%d", limit))
	endpoint.RawQuery = params.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("X-Subscription-Token", b.apiKey)
	request.Header.Set("Accept", "application/json")

	response, err := b.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("brave search: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("brave search http %d", response.StatusCode)
	}

	var payload struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("brave search decode: %w", err)
	}

	results := make([]ports.SearchResult, 0, len(payload.Web.Results))
	for _, result := range payload.Web.Results {
		results = append(results, ports.SearchResult{
			Title:   result.Title,
			URL:     result.URL,
			Snippet: result.Description,
		})
	}
	return results, nil
}

var _ ports.SearchProvider = (*BraveSearch)(nil)
