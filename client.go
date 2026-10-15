package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	defaultMaxResponseBytes = 10 << 20 // 10 MiB
	maxErrorBodyBytes       = 512
)

// client calls the Misskey API. It has no timeout of its own: callers bound
// each request with the context they pass in.
type client struct {
	baseURL          string // without a trailing slash, see normalizeBaseURL
	apiKey           string
	http             *http.Client
	maxResponseBytes int64
}

func newClient(baseURL, apiKey string) *client {
	return &client{
		baseURL:          baseURL,
		apiKey:           apiKey,
		http:             &http.Client{},
		maxResponseBytes: defaultMaxResponseBytes,
	}
}

// Post sends an empty JSON object to path (for example "/api/admin/queue/stats")
// and decodes the JSON response into out. Errors never contain the API key.
func (c *client) Post(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader("{}"))
	if err != nil {
		return fmt.Errorf("create request for %s: %w", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return fmt.Errorf("request %s: unexpected status %d: %s", path, resp.StatusCode, bytes.TrimSpace(snippet))
	}

	// Read one byte more than allowed to tell "exactly the limit" from "over".
	data, err := io.ReadAll(io.LimitReader(resp.Body, c.maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read response of %s: %w", path, err)
	}
	if int64(len(data)) > c.maxResponseBytes {
		return fmt.Errorf("response of %s exceeds %d bytes", path, c.maxResponseBytes)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response of %s: %w", path, err)
	}
	return nil
}
