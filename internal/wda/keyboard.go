package wda

import (
	"context"
	"fmt"
)

// SendKeys types the provided string by forwarding it as a sequence of keys to the WDA.
func (c *Client) SendKeys(ctx context.Context, text string) error {
	if c.sessionID == "" {
		return fmt.Errorf("wda keyboard: WDA session not active")
	}

	// WDA expects characters to be sent in an array of strings
	chars := make([]string, 0, len(text))
	for _, r := range text {
		chars = append(chars, string(r))
	}

	body := map[string]interface{}{
		"value": chars,
	}

	urlPath := fmt.Sprintf("/session/%s/wda/keys", c.sessionID)
	if err := c.Request(ctx, "POST", urlPath, body, nil); err != nil {
		return fmt.Errorf("wda keyboard send keys failed: %w", err)
	}
	return nil
}
