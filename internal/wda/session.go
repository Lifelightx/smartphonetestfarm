// Package wda implements WebDriverAgent (WDA) client for iOS automation and interaction.
//
// File: session.go
// This file contains implementation and helper structures for WebDriverAgent (WDA) client for iOS automation and interaction.

package wda

import (
	"context"
	"fmt"
)

// SessionResponse represents the response body returned when creating a session.
type SessionResponse struct {
	Value struct {
		SessionID    string                 `json:"sessionId"`
		Capabilities map[string]interface{} `json:"capabilities"`
	} `json:"value"`
}

// StatusResponse represents the response body from a WDA status query.
type StatusResponse struct {
	Value struct {
		Ready bool `json:"ready"`
	} `json:"value"`
}

// CreateSession establishes a new session on the WDA server.
func (c *Client) CreateSession(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.createSessionLocked(ctx)
}

// ios fps setting
// createSessionLocked establishes a session. Caller must hold c.mu.Lock().
func (c *Client) createSessionLocked(ctx context.Context) (string, error) {
	body := map[string]interface{}{
		"capabilities": map[string]interface{}{
			"alwaysMatch": map[string]interface{}{
				"mjpegServerFramerate": 30,
				"mjpegScalingFactor":   50,
			},
		},
	}

	var resp SessionResponse
	if err := c.Request(ctx, "POST", "/session", body, &resp); err != nil {
		return "", fmt.Errorf("failed to create WDA session: %w", err)
	}

	c.sessionID = resp.Value.SessionID

	// Update WDA settings to optimize MJPEG stream performance (best-effort)
	settingsBody := map[string]interface{}{
		"settings": map[string]interface{}{
			"mjpegServerScreenshotQuality": 60,
			"mjpegScalingFactor":           50,
		},
	}
	settingsPath := fmt.Sprintf("/session/%s/settings", resp.Value.SessionID)
	_ = c.Request(ctx, "POST", settingsPath, settingsBody, nil)

	return resp.Value.SessionID, nil
}

// EnsureSession makes sure a valid WDA session is active on the server.
// It is thread-safe and prevents concurrent session creation attempts.
func (c *Client) EnsureSession(ctx context.Context) error {
	c.mu.RLock()
	hasSession := c.sessionID != ""
	c.mu.RUnlock()

	if hasSession {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double check under write lock
	if c.sessionID != "" {
		return nil
	}

	_, err := c.createSessionLocked(ctx)
	return err
}

// DeleteSession closes the current session on the WDA server.
func (c *Client) DeleteSession(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sessionID == "" {
		return nil
	}

	urlPath := fmt.Sprintf("/session/%s", c.sessionID)
	if err := c.Request(ctx, "DELETE", urlPath, nil, nil); err != nil {
		return fmt.Errorf("failed to delete WDA session %s: %w", c.sessionID, err)
	}

	c.sessionID = ""
	return nil
}

// Status queries the WDA server status to check if it is ready to receive requests.
func (c *Client) Status(ctx context.Context) (bool, error) {
	var resp StatusResponse
	if err := c.Request(ctx, "GET", "/status", nil, &resp); err != nil {
		return false, fmt.Errorf("WDA status check failed: %w", err)
	}
	return resp.Value.Ready, nil
}

// SizeResponse represents the screen size response from WDA.
type SizeResponse struct {
	Value struct {
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	} `json:"value"`
}

// GetWindowSize returns the logical width and height of the screen in points.
func (c *Client) GetWindowSize(ctx context.Context) (float64, float64, error) {
	if c.sessionID == "" {
		return 0, 0, fmt.Errorf("wda window size: WDA session not active")
	}
	var resp SizeResponse
	urlPath := fmt.Sprintf("/session/%s/window/size", c.sessionID)
	if err := c.Request(ctx, "GET", urlPath, nil, &resp); err != nil {
		return 0, 0, fmt.Errorf("failed to get window size: %w", err)
	}
	return resp.Value.Width, resp.Value.Height, nil
}
