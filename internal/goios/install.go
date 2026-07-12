// Package goios implements native iOS communication using the go-ios library.
//
// File: install.go
// This file contains implementation and helper structures for native iOS communication using the go-ios library.

package goios

import (
	"context"
	"fmt"
)

// Install installs an app bundle or IPA onto the iOS device.
func (am *AppManager) Install(ctx context.Context, serial string, appPath string) error {
	_, err := am.client.Run(ctx, serial, "install", "--path="+appPath)
	if err != nil {
		return fmt.Errorf("goios install failed: %w", err)
	}
	return nil
}

// Uninstall uninstalls an app by bundle ID from the iOS device.
func (am *AppManager) Uninstall(ctx context.Context, serial string, appID string) error {
	_, err := am.client.Run(ctx, serial, "uninstall", appID)
	if err != nil {
		return fmt.Errorf("goios uninstall failed: %w", err)
	}
	return nil
}

// IOSAppInfo contains the standard bundle info returned by the apps command.
type IOSAppInfo struct {
	BundleID    string `json:"CFBundleIdentifier"`
	DisplayName string `json:"CFBundleDisplayName"`
}

// List returns a list of installed app bundle IDs on the iOS device.
func (am *AppManager) List(ctx context.Context, serial string) ([]string, error) {
	out, err := am.client.Run(ctx, serial, "apps")
	if err != nil {
		return nil, fmt.Errorf("goios list apps failed: %w", err)
	}

	apps, mapApps, err := parseIOSAppList(string(out))
	if err != nil {
		return nil, fmt.Errorf("goios list apps parse failed: %w", err)
	}

	var list []string
	if apps != nil {
		for _, app := range apps {
			if app.BundleID != "" {
				list = append(list, app.BundleID)
			}
		}
	} else if mapApps != nil {
		for k := range mapApps {
			list = append(list, k)
		}
	}

	return list, nil
}
