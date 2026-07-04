package wda

import (
	"context"
	"fmt"
)

// LaunchApp requests WDA to launch the application specified by the bundle ID.
func (c *Client) LaunchApp(ctx context.Context, bundleID string) error {
	if c.sessionID == "" {
		return fmt.Errorf("wda app: session not active")
	}

	body := map[string]interface{}{
		"bundleId": bundleID,
	}

	urlPath := fmt.Sprintf("/session/%s/wda/apps/launch", c.sessionID)
	if err := c.Request(ctx, "POST", urlPath, body, nil); err != nil {
		return fmt.Errorf("failed to launch app %s via WDA: %w", bundleID, err)
	}

	return nil
}

// TerminateApp requests WDA to stop/terminate the application specified by the bundle ID.
func (c *Client) TerminateApp(ctx context.Context, bundleID string) error {
	if c.sessionID == "" {
		return fmt.Errorf("wda app: session not active")
	}

	body := map[string]interface{}{
		"bundleId": bundleID,
	}

	urlPath := fmt.Sprintf("/session/%s/wda/apps/terminate", c.sessionID)
	if err := c.Request(ctx, "POST", urlPath, body, nil); err != nil {
		return fmt.Errorf("failed to terminate app %s via WDA: %w", bundleID, err)
	}

	return nil
}

// OpenURL requests WDA to open the specified URL (supports deep links and http/https).
func (c *Client) OpenURL(ctx context.Context, urlStr string) error {
	if c.sessionID == "" {
		return fmt.Errorf("wda app: session not active")
	}

	body := map[string]interface{}{
		"url": urlStr,
	}

	urlPath := fmt.Sprintf("/session/%s/url", c.sessionID)
	if err := c.Request(ctx, "POST", urlPath, body, nil); err != nil {
		return fmt.Errorf("failed to open URL %s via WDA: %w", urlStr, err)
	}

	return nil
}

