package ports

import "context"

// SearchResult es un resultado de búsqueda web.
type SearchResult struct {
	Title   string
	URL     string
	Snippet string
}

// SearchProvider es el puerto hacia un motor de búsqueda.
type SearchProvider interface {
	// Search devuelve hasta limit resultados para la consulta.
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}
