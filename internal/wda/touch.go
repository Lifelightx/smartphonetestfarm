package wda

import (
	"context"
	"fmt"
)

// Tap sends a single tap command using W3C Actions (extremely low latency touch injection).
func (c *Client) Tap(ctx context.Context, x, y float64) error {
	if c.sessionID == "" {
		return fmt.Errorf("wda tap: WDA session not active")
	}

	body := map[string]interface{}{
		"actions": []interface{}{
			map[string]interface{}{
				"type": "pointer",
				"id":   "finger1",
				"parameters": map[string]interface{}{
					"pointerType": "touch",
				},
				"actions": []interface{}{
					map[string]interface{}{"type": "pointerMove", "duration": 0, "x": x, "y": y},
					map[string]interface{}{"type": "pointerDown", "button": 0},
					map[string]interface{}{"type": "pause", "duration": 50},
					map[string]interface{}{"type": "pointerUp", "button": 0},
				},
			},
		},
	}

	urlPath := fmt.Sprintf("/session/%s/actions", c.sessionID)
	if err := c.Request(ctx, "POST", urlPath, body, nil); err != nil {
		return fmt.Errorf("wda tap failed: %w", err)
	}
	return nil
}

// Swipe drags from starting coordinates to ending coordinates over the duration using W3C Actions.
func (c *Client) Swipe(ctx context.Context, x1, y1, x2, y2 float64, durationSec float64) error {
	if c.sessionID == "" {
		return fmt.Errorf("wda swipe: WDA session not active")
	}

	durationMs := int(durationSec * 1000)
	if durationMs <= 0 {
		durationMs = 200
	}

	body := map[string]interface{}{
		"actions": []interface{}{
			map[string]interface{}{
				"type": "pointer",
				"id":   "finger1",
				"parameters": map[string]interface{}{
					"pointerType": "touch",
				},
				"actions": []interface{}{
					map[string]interface{}{"type": "pointerMove", "duration": 0, "x": x1, "y": y1},
					map[string]interface{}{"type": "pointerDown", "button": 0},
					map[string]interface{}{"type": "pointerMove", "duration": durationMs, "x": x2, "y": y2},
					map[string]interface{}{"type": "pointerUp", "button": 0},
				},
			},
		},
	}

	urlPath := fmt.Sprintf("/session/%s/actions", c.sessionID)
	if err := c.Request(ctx, "POST", urlPath, body, nil); err != nil {
		return fmt.Errorf("wda swipe failed: %w", err)
	}
	return nil
}
