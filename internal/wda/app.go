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
