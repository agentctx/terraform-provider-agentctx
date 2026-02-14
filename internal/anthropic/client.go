package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

const (
	defaultBaseURL        = "https://api.anthropic.com"
	defaultMaxRetries     = 3
	defaultTimeoutSeconds = 30
	anthropicVersion      = "2023-06-01"
	anthropicBeta         = "skills-2025-10-02"
)

// ClientConfig holds configuration for constructing a new Client.
type ClientConfig struct {
	APIKey         string
	MaxRetries     int
	TimeoutSeconds int
	DestroyRemote  bool
	BaseURL        string
}

// Client is an HTTP client for the Anthropic Skills API.
type Client struct {
	httpClient    *http.Client
	apiKey        string
	maxRetries    int
	destroyRemote bool
	baseURL       string
}

// NewClient creates a new Anthropic API client from the given configuration.
func NewClient(cfg ClientConfig) *Client {
	maxRetries := cfg.MaxRetries
	if maxRetries < 0 {
		maxRetries = defaultMaxRetries
	}

	timeoutSec := cfg.TimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = defaultTimeoutSeconds
	}

	baseURL := defaultBaseURL
	if cfg.BaseURL != "" {
		baseURL = cfg.BaseURL
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSec) * time.Second,
		},
		apiKey:        cfg.APIKey,
		maxRetries:    maxRetries,
		destroyRemote: cfg.DestroyRemote,
		baseURL:       baseURL,
	}
}

// DestroyRemote reports whether the client is configured to destroy remote
// resources on deletion.
func (c *Client) DestroyRemote() bool {
	return c.destroyRemote
}

// do performs an HTTP request with JSON encoding/decoding and retry logic.
// method is the HTTP method, path is appended to the base URL, body is
// JSON-encoded as the request body (nil for no body), and result is decoded
// from the response body (nil to discard the response).
func (c *Client) do(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	url := c.baseURL + path

	var bodyReader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("anthropic: marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(encoded)
	}

	var lastErr error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s, ...
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}

			// Reset the body reader for retry.
			if body != nil {
				encoded, _ := json.Marshal(body)
				bodyReader = bytes.NewReader(encoded)
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return fmt.Errorf("anthropic: create request: %w", err)
		}

		req.Header.Set("x-api-key", c.apiKey)
		req.Header.Set("anthropic-version", anthropicVersion)
		if anthropicBeta != "" {
			req.Header.Set("anthropic-beta", anthropicBeta)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("anthropic: request failed: %w", err)
			// Network errors are retryable.
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("anthropic: read response body: %w", err)
			continue
		}

		// Success.
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if result != nil && len(respBody) > 0 {
				if err := json.Unmarshal(respBody, result); err != nil {
					return fmt.Errorf("anthropic: decode response: %w", err)
				}
			}
			return nil
		}

		// Parse error response.
		apiErr := parseAPIError(resp.StatusCode, respBody)

		// Retry on 429 (rate limit) and 5xx (server errors).
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			lastErr = apiErr
			continue
		}

		// Non-retryable error.
		return apiErr
	}

	// All retries exhausted.
	if lastErr != nil {
		return fmt.Errorf("anthropic: request failed after %d retries: %w", c.maxRetries, lastErr)
	}
	return fmt.Errorf("anthropic: request failed after %d retries", c.maxRetries)
}

// doRaw performs an HTTP request and returns the raw response body bytes.
// It uses the same retry logic as do but does not JSON-decode the response.
func (c *Client) doRaw(ctx context.Context, method, path string) ([]byte, error) {
	url := c.baseURL + path

	var lastErr error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return nil, fmt.Errorf("anthropic: create request: %w", err)
		}

		req.Header.Set("x-api-key", c.apiKey)
		req.Header.Set("anthropic-version", anthropicVersion)
		if anthropicBeta != "" {
			req.Header.Set("anthropic-beta", anthropicBeta)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("anthropic: request failed: %w", err)
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("anthropic: read response body: %w", err)
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return respBody, nil
		}

		apiErr := parseAPIError(resp.StatusCode, respBody)

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			lastErr = apiErr
			continue
		}

		return nil, apiErr
	}

	if lastErr != nil {
		return nil, fmt.Errorf("anthropic: request failed after %d retries: %w", c.maxRetries, lastErr)
	}
	return nil, fmt.Errorf("anthropic: request failed after %d retries", c.maxRetries)
}

// doMultipart performs an HTTP request with a pre-built body (for multipart
// uploads) and retry logic. The buildBody function is called on each attempt
// to produce a fresh body reader and the Content-Type header value.
func (c *Client) doMultipart(ctx context.Context, method, path string, buildBody func() (io.Reader, string, error), result interface{}) error {
	url := c.baseURL + path

	var lastErr error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		bodyReader, contentType, err := buildBody()
		if err != nil {
			return fmt.Errorf("anthropic: build multipart body: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return fmt.Errorf("anthropic: create request: %w", err)
		}

		req.Header.Set("x-api-key", c.apiKey)
		req.Header.Set("anthropic-version", anthropicVersion)
		if anthropicBeta != "" {
			req.Header.Set("anthropic-beta", anthropicBeta)
		}
		req.Header.Set("Content-Type", contentType)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("anthropic: request failed: %w", err)
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("anthropic: read response body: %w", err)
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if result != nil && len(respBody) > 0 {
				if err := json.Unmarshal(respBody, result); err != nil {
					return fmt.Errorf("anthropic: decode response: %w", err)
				}
			}
			return nil
		}

		apiErr := parseAPIError(resp.StatusCode, respBody)

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			lastErr = apiErr
			continue
		}

		return apiErr
	}

	if lastErr != nil {
		return fmt.Errorf("anthropic: request failed after %d retries: %w", c.maxRetries, lastErr)
	}
	return fmt.Errorf("anthropic: request failed after %d retries", c.maxRetries)
}

// parseAPIError parses an error response body into an APIError.
// It handles both flat format {"type":"...","message":"..."} and the nested
// Anthropic format {"type":"error","error":{"type":"...","message":"..."}}.
func parseAPIError(statusCode int, body []byte) *APIError {
	apiErr := &APIError{StatusCode: statusCode}

	// Try nested format first: {"type":"error","error":{...}}
	var nested struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &nested) == nil && nested.Error.Message != "" {
		apiErr.Type = nested.Error.Type
		apiErr.Message = nested.Error.Message
		return apiErr
	}

	// Fall back to flat format: {"type":"...","message":"..."}
	_ = json.Unmarshal(body, apiErr)
	return apiErr
}
