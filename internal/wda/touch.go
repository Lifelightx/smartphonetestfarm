package wda

import (
	"context"
	"fmt"
)

// Tap sends a single tap command to specific coordinate coordinates.
func (c *Client) Tap(ctx context.Context, x, y float64) error {
	if c.sessionID == "" {
		return fmt.Errorf("wda tap: WDA session not active")
	}

	body := map[string]interface{}{
		"x": x,
		"y": y,
	}

	urlPath := fmt.Sprintf("/session/%s/wda/tap", c.sessionID)
	if err := c.Request(ctx, "POST", urlPath, body, nil); err != nil {
		return fmt.Errorf("wda tap failed: %w", err)
	}
	return nil
}

// Swipe drags from starting coordinates (x1, y1) to ending coordinates (x2, y2) over the duration.
func (c *Client) Swipe(ctx context.Context, x1, y1, x2, y2 float64, durationSec float64) error {
	if c.sessionID == "" {
		return fmt.Errorf("wda swipe: WDA session not active")
	}

	body := map[string]interface{}{
		"fromX":    x1,
		"fromY":    y1,
		"toX":      x2,
		"toY":      y2,
		"duration": durationSec,
	}

	urlPath := fmt.Sprintf("/session/%s/wda/dragfromtoforduration", c.sessionID)
	if err := c.Request(ctx, "POST", urlPath, body, nil); err != nil {
		return fmt.Errorf("wda swipe failed: %w", err)
	}
	return nil
}
