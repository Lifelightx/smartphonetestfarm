// Package wda implements WebDriverAgent (WDA) client for iOS automation and interaction.
//
// File: keyboard.go
// This file contains implementation and helper structures for WebDriverAgent (WDA) client for iOS automation and interaction.

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

// PressButton simulates pressing a physical device button (e.g. "volumeup", "volumedown", "home").
func (c *Client) PressButton(ctx context.Context, name string) error {
	if c.sessionID == "" {
		return fmt.Errorf("wda button: WDA session not active")
	}

	body := map[string]interface{}{
		"name": name,
	}

	urlPath := fmt.Sprintf("/session/%s/wda/pressButton", c.sessionID)
	if err := c.Request(ctx, "POST", urlPath, body, nil); err != nil {
		return fmt.Errorf("wda press button %q failed: %w", name, err)
	}
	return nil
}
