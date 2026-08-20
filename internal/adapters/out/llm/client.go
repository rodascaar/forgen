// Package llm implementa los adapters de proveedores LLM.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxRetries es el número de reintentos para peticiones idempotentes.
const maxRetries = 4

// baseBackoff es la espera inicial entre reintentos.
const baseBackoff = 500 * time.Millisecond

// clientTimeout limita la duración total de cada intento HTTP.
const clientTimeout = 5 * time.Minute

// Client es un cliente HTTP compartido con retry exponencial y fail-fast.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
	Logger  *slog.Logger
	// ExtraHeaders se añade a todas las peticiones (ej: phase, tags).
	ExtraHeaders map[string]string
}

// NewClient construye el cliente HTTP con timeout.
func NewClient(baseURL, apiKey string, logger *slog.Logger) *Client {
	return &Client{
		BaseURL:      strings.TrimSuffix(baseURL, "/"),
		APIKey:       apiKey,
		HTTP:         &http.Client{Timeout: clientTimeout},
		Logger:       logger,
		ExtraHeaders: map[string]string{},
	}
}

// Do envía una petición con retry; el body debe serializarse de nuevo en cada
// reintento (la función jsonBody se invoca por intento).
func (c *Client) Do(ctx context.Context, method, path string, jsonBody func() ([]byte, error)) (*http.Response, error) {
	var lastErr error
	delay := baseBackoff

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			if !waitForRetry(ctx, delay, lastErr) {
				return nil, fmt.Errorf("cancelado durante retry: %w", ctx.Err())
			}
			delay *= 2
		}

		body, err := jsonBody()
		if err != nil {
			return nil, fmt.Errorf("serializar cuerpo: %w", err)
		}

		request, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("construir petición: %w", err)
		}
		request.Header.Set("Content-Type", "application/json")
		if c.APIKey != "" {
			request.Header.Set("Authorization", "Bearer "+c.APIKey)
		}
		for key, value := range c.ExtraHeaders {
			request.Header.Set(key, value)
		}

		response, err := c.HTTP.Do(request)
		if err != nil {
			lastErr = fmt.Errorf("red: %w", err)
			if c.Logger != nil {
				c.Logger.Warn("llm.request.retry", "attempt", attempt, "err", lastErr)
			}
			continue
		}

		if shouldRetry(response.StatusCode) {
			lastErr = fmt.Errorf("http %d", response.StatusCode)
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if c.Logger != nil {
				c.Logger.Warn("llm.request.retry", "attempt", attempt, "status", response.StatusCode)
			}
			if retryAfter := retryAfterDelay(response); retryAfter > 0 {
				delay = retryAfter
			}
			continue
		}

		if response.StatusCode >= 400 {
			bodyText, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
			_ = response.Body.Close()
			return nil, fmt.Errorf("llm http %d: %s", response.StatusCode, strings.TrimSpace(string(bodyText)))
		}

		return response, nil
	}
	return nil, fmt.Errorf("llm: sin éxito tras %d intentos: %w", maxRetries, lastErr)
}

func shouldRetry(statusCode int) bool {
	switch statusCode {
	case http.StatusTooManyRequests, http.StatusRequestTimeout,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func retryAfterDelay(response *http.Response) time.Duration {
	header := response.Header.Get("Retry-After")
	if header == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(header); err == nil {
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(header); err == nil {
		return time.Until(date)
	}
	return 0
}

func waitForRetry(ctx context.Context, delay time.Duration, lastErr error) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(delay):
		return true
	}
}

// StreamSSE lee un body SSE línea a línea y llama a onLine por cada data.
// Devuelve [DONE] como error centinel para cortar limpiamente.
func (c *Client) StreamSSE(body io.Reader, onData func(data string) error) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return nil
		}
		if data == "" {
			continue
		}
		if err := onData(data); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// postJSON es un helper para peticiones JSON con cuerpo fijo.
func postJSON(ctx context.Context, client *Client, path string, payload any) (*http.Response, error) {
	return client.Do(ctx, http.MethodPost, path, func() ([]byte, error) {
		return json.Marshal(payload)
	})
}
