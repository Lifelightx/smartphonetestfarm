package wda

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Client handles low-level HTTP interactions with the WebDriverAgent (WDA) server.
type Client struct {
	baseURL    string
	httpClient *http.Client
	mu         sync.RWMutex
	sessionID  string
}

// NewClient creates a new WDA HTTP client targeting the WDA local port.
func NewClient(port int) *Client {
	return &Client{
		baseURL: fmt.Sprintf("http://127.0.0.1:%d", port),
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// SetSessionID updates the active WDA session ID.
func (c *Client) SetSessionID(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionID = id
}

// SessionID returns the currently set WDA session ID.
func (c *Client) SessionID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionID
}

// Request executes an HTTP request to WebDriverAgent.
func (c *Client) Request(ctx context.Context, method, path string, body interface{}, responseVal interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("wda client: marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("wda client: create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("wda client: execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("wda client: server returned status %d: %s", resp.StatusCode, string(respBody))
	}

	if responseVal != nil {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("wda client: read response body: %w", err)
		}
		if err := json.Unmarshal(respBody, responseVal); err != nil {
			return fmt.Errorf("wda client: unmarshal response: %w (raw: %s)", err, string(respBody))
		}
	}

	return nil
}
