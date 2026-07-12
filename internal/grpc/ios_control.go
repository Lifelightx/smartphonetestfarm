// Package grpc implements gRPC servers and service handlers for device control.
//
// File: ios_control.go
// This file contains implementation and helper structures for gRPC servers and service handlers for device control.

package grpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"protean-provider/internal/goios"
	provider "protean-provider/pkg/protocol/provider"
)

// handleIOSControl routes interactive device control events for iOS devices using WDA.
func (s *Server) handleIOSControl(ctx context.Context, req *provider.ControlRequest, serial string, lastX, lastY *int32, touchDownTime *time.Time) (*provider.ControlResponse, error) {
	wdaClient, err := s.sup.WDAClient(serial)
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "failed to get WDA client for iOS device %s: %v", serial, err)
	}

	ensureSession := func() error {
		return wdaClient.EnsureSession(ctx)
	}

	var resp *provider.ControlResponse

	switch event := req.Event.(type) {
	case *provider.ControlRequest_Touch:
		t := event.Touch
		switch t.Action {
		case provider.TouchEvent_DOWN:
			*lastX = t.X
			*lastY = t.Y
			*touchDownTime = time.Now()
			resp = &provider.ControlResponse{Success: true}
		case provider.TouchEvent_MOVE:
			*lastX = t.X
			*lastY = t.Y
			resp = &provider.ControlResponse{Success: true}
		case provider.TouchEvent_UP:
			duration := time.Since(*touchDownTime).Milliseconds()
			if err := ensureSession(); err != nil {
				resp = &provider.ControlResponse{Success: false, Message: err.Error()}
			} else {
				if duration < 250 && abs(*lastX-t.X) < 10 && abs(*lastY-t.Y) < 10 {
					// Tap
					tapErr := wdaClient.Tap(ctx, float64(t.X), float64(t.Y))
					if tapErr != nil {
						resp = &provider.ControlResponse{Success: false, Message: tapErr.Error()}
					} else {
						resp = &provider.ControlResponse{Success: true}
					}
				} else {
					// Swipe
					durationSec := float64(duration) / 1000.0
					if durationSec <= 0 {
						durationSec = 0.5
					}
					swipeErr := wdaClient.Swipe(ctx, float64(*lastX), float64(*lastY), float64(t.X), float64(t.Y), durationSec)
					if swipeErr != nil {
						resp = &provider.ControlResponse{Success: false, Message: swipeErr.Error()}
					} else {
						resp = &provider.ControlResponse{Success: true}
					}
				}
			}
		}

	case *provider.ControlRequest_Key:
		k := event.Key
		if k.Action == provider.KeyEvent_DOWN {
			if err := ensureSession(); err != nil {
				resp = &provider.ControlResponse{Success: false, Message: err.Error()}
			} else {
				switch k.KeyCode {
				case 3: // Home key
					err := wdaClient.Request(ctx, "POST", "/wda/homescreen", nil, nil)
					if err != nil {
						resp = &provider.ControlResponse{Success: false, Message: err.Error()}
					} else {
						resp = &provider.ControlResponse{Success: true}
					}
				case 24: // Volume Up
					err := wdaClient.PressButton(ctx, "volumeup")
					if err != nil {
						resp = &provider.ControlResponse{Success: false, Message: err.Error()}
					} else {
						resp = &provider.ControlResponse{Success: true}
					}
				case 25: // Volume Down
					err := wdaClient.PressButton(ctx, "volumedown")
					if err != nil {
						resp = &provider.ControlResponse{Success: false, Message: err.Error()}
					} else {
						resp = &provider.ControlResponse{Success: true}
					}
				case 164: // Volume Mute
					// WDA doesn't support mute natively, try it but ignore error or fallback
					_ = wdaClient.PressButton(ctx, "mute")
					resp = &provider.ControlResponse{Success: true}
				default:
					resp = &provider.ControlResponse{Success: true}
				}
			}
		} else {
			resp = &provider.ControlResponse{Success: true}
		}

	case *provider.ControlRequest_Text:
		t := event.Text
		if err := ensureSession(); err != nil {
			resp = &provider.ControlResponse{Success: false, Message: err.Error()}
		} else {
			err := wdaClient.SendKeys(ctx, t.Text)
			if err != nil {
				resp = &provider.ControlResponse{Success: false, Message: err.Error()}
			} else {
				resp = &provider.ControlResponse{Success: true}
			}
		}

	case *provider.ControlRequest_Rotate:
		// Rotation not supported directly on iOS via endpoint
		resp = &provider.ControlResponse{Success: true}

	case *provider.ControlRequest_Shell:
		cmd := strings.TrimSpace(event.Shell.Command)
		if cmd == "" {
			return &provider.ControlResponse{Success: false, Message: "Empty command"}, nil
		}

		if cmd == "reboot" {
			goiosClient := goios.NewClient()
			_, err := goiosClient.Run(ctx, serial, "reboot")
			if err != nil {
				resp = &provider.ControlResponse{
					Success: false,
					Message: fmt.Sprintf("reboot failed: %v", err),
				}
			} else {
				resp = &provider.ControlResponse{
					Success: true,
					Message: "Reboot initiated",
				}
			}
		} else if strings.HasPrefix(cmd, "am start ") {
			if err := ensureSession(); err != nil {
				resp = &provider.ControlResponse{Success: false, Message: err.Error()}
			} else {
				if strings.Contains(cmd, "android.settings.SETTINGS") {
					err := wdaClient.LaunchApp(ctx, "com.apple.Preferences")
					if err != nil {
						resp = &provider.ControlResponse{Success: false, Message: fmt.Sprintf("failed to launch Settings: %v", err)}
					} else {
						resp = &provider.ControlResponse{Success: true, Message: "Settings launched"}
					}
				} else if strings.Contains(cmd, "market://details?") {
					err := wdaClient.LaunchApp(ctx, "com.apple.AppStore")
					if err != nil {
						resp = &provider.ControlResponse{Success: false, Message: fmt.Sprintf("failed to launch App Store: %v", err)}
					} else {
						resp = &provider.ControlResponse{Success: true, Message: "App Store launched"}
					}
				} else if strings.Contains(cmd, "android.settings.LOCALE_SETTINGS") {
					err := wdaClient.OpenURL(ctx, "App-Prefs:root=General&path=INTERNATIONAL")
					if err != nil {
						err = wdaClient.LaunchApp(ctx, "com.apple.Preferences")
					}
					if err != nil {
						resp = &provider.ControlResponse{Success: false, Message: fmt.Sprintf("failed to open Settings: %v", err)}
					} else {
						resp = &provider.ControlResponse{Success: true, Message: "Language settings opened"}
					}
				} else if strings.Contains(cmd, "android.settings.WIFI_SETTINGS") {
					err := wdaClient.OpenURL(ctx, "App-Prefs:root=WIFI")
					if err != nil {
						err = wdaClient.LaunchApp(ctx, "com.apple.Preferences")
					}
					if err != nil {
						resp = &provider.ControlResponse{Success: false, Message: fmt.Sprintf("failed to open Settings: %v", err)}
					} else {
						resp = &provider.ControlResponse{Success: true, Message: "WiFi settings opened"}
					}
				} else if strings.Contains(cmd, "android.settings.MANAGE_APPLICATIONS_SETTINGS") {
					err := wdaClient.OpenURL(ctx, "App-Prefs:root=General&path=STORAGE_AND_BACKUP")
					if err != nil {
						err = wdaClient.LaunchApp(ctx, "com.apple.Preferences")
					}
					if err != nil {
						resp = &provider.ControlResponse{Success: false, Message: fmt.Sprintf("failed to open Settings: %v", err)}
					} else {
						resp = &provider.ControlResponse{Success: true, Message: "Storage settings opened"}
					}
				} else if strings.Contains(cmd, "android.settings.APPLICATION_DEVELOPMENT_SETTINGS") {
					err := wdaClient.OpenURL(ctx, "App-Prefs:root=DEVELOPER")
					if err != nil {
						err = wdaClient.LaunchApp(ctx, "com.apple.Preferences")
					}
					if err != nil {
						resp = &provider.ControlResponse{Success: false, Message: fmt.Sprintf("failed to open Settings: %v", err)}
					} else {
						resp = &provider.ControlResponse{Success: true, Message: "Developer settings opened"}
					}
				} else if strings.Contains(cmd, "-d ") {
					parts := strings.SplitN(cmd, "-d ", 2)
					if len(parts) == 2 {
						urlPart := parts[1]
						var urlStr string
						if strings.HasPrefix(urlPart, `"`) {
							subParts := strings.SplitN(urlPart[1:], `"`, 2)
							urlStr = subParts[0]
						} else if strings.HasPrefix(urlPart, `'`) {
							subParts := strings.SplitN(urlPart[1:], `'`, 2)
							urlStr = subParts[0]
						} else {
							subParts := strings.Fields(urlPart)
							if len(subParts) > 0 {
								urlStr = subParts[0]
							}
						}
						urlStr = strings.TrimSpace(urlStr)
						err := wdaClient.OpenURL(ctx, urlStr)
						if err != nil {
							resp = &provider.ControlResponse{Success: false, Message: fmt.Sprintf("failed to open URL: %v", err)}
						} else {
							resp = &provider.ControlResponse{Success: true, Message: fmt.Sprintf("Opened URL: %s", urlStr)}
						}
					}
				} else {
					resp = &provider.ControlResponse{
						Success: false,
						Message: fmt.Sprintf("Unsupported intent action for iOS: %s", cmd),
					}
				}
			}
		} else {
			resp = &provider.ControlResponse{
				Success: false,
				Message: fmt.Sprintf("Command %q is not supported on iOS", cmd),
			}
		}
	}

	return resp, nil
}
