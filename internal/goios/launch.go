package goios

import (
	"context"
	"fmt"
)

// AppManager implements platform.AppManager for iOS.
type AppManager struct {
	client Client
}

// NewAppManager creates a new AppManager.
func NewAppManager(client Client) *AppManager {
	return &AppManager{client: client}
}

// Launch launches an app by its bundle ID on the iOS device.
func (am *AppManager) Launch(ctx context.Context, serial string, appID string) error {
	_, err := am.client.Run(ctx, serial, "launch", appID)
	if err != nil {
		return fmt.Errorf("goios launch failed: %w", err)
	}
	return nil
}

// Stop stops a running app by its bundle ID on the iOS device.
func (am *AppManager) Stop(ctx context.Context, serial string, appID string) error {
	_, err := am.client.Run(ctx, serial, "kill", appID)
	if err != nil {
		return fmt.Errorf("goios kill failed: %w", err)
	}
	return nil
}
