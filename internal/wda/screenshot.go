package wda

import (
	"context"
	"encoding/base64"
	"fmt"
)

// ScreenshotResponse holds the base64-encoded image string returned by WDA.
type ScreenshotResponse struct {
	Value string `json:"value"`
}

// Screenshot requests a screen capture from WDA and returns the decoded PNG bytes.
func (c *Client) Screenshot(ctx context.Context) ([]byte, error) {
	urlPath := "/screenshot"
	if c.sessionID != "" {
		urlPath = fmt.Sprintf("/session/%s/screenshot", c.sessionID)
	}

	var resp ScreenshotResponse
	if err := c.Request(ctx, "GET", urlPath, nil, &resp); err != nil {
		return nil, fmt.Errorf("wda screenshot request failed: %w", err)
	}

	imgBytes, err := base64.StdEncoding.DecodeString(resp.Value)
	if err != nil {
		return nil, fmt.Errorf("failed to decode WDA screenshot base64 response: %w", err)
	}

	return imgBytes, nil
}
