// Package grpc implements gRPC servers and service handlers for device control.
//
// File: android_control.go
// This file contains implementation and helper structures for gRPC servers and service handlers for device control.

package grpc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	provider "protean-provider/pkg/protocol/provider"
)

// handleAndroidControl routes interactive device control events for Android devices.
func (s *Server) handleAndroidControl(ctx context.Context, req *provider.ControlRequest, serial string, lastX, lastY *int32, touchDownTime *time.Time) (*provider.ControlResponse, error) {
	agt := s.sup.Agent(serial)
	if agt == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "no active agent session found for device %s", serial)
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
			if duration < 250 && abs(*lastX-t.X) < 10 && abs(*lastY-t.Y) < 10 {
				// Tap event
				cmd := fmt.Sprintf("input tap %d %d", t.X, t.Y)
				_, shellErr := agt.Shell(ctx, cmd)
				if shellErr != nil {
					resp = &provider.ControlResponse{Success: false, Message: shellErr.Error()}
				} else {
					resp = &provider.ControlResponse{Success: true}
				}
			} else {
				// Swipe event
				if duration == 0 {
					duration = 100
				}
				cmd := fmt.Sprintf("input swipe %d %d %d %d %d", *lastX, *lastY, t.X, t.Y, duration)
				_, shellErr := agt.Shell(ctx, cmd)
				if shellErr != nil {
					resp = &provider.ControlResponse{Success: false, Message: shellErr.Error()}
				} else {
					resp = &provider.ControlResponse{Success: true}
				}
			}
		}

	case *provider.ControlRequest_Key:
		k := event.Key
		if k.Action == provider.KeyEvent_DOWN {
			cmd := fmt.Sprintf("input keyevent %d", k.KeyCode)
			_, shellErr := agt.Shell(ctx, cmd)
			if shellErr != nil {
				resp = &provider.ControlResponse{Success: false, Message: shellErr.Error()}
			} else {
				resp = &provider.ControlResponse{Success: true}
			}
		} else {
			resp = &provider.ControlResponse{Success: true}
		}

	case *provider.ControlRequest_Text:
		t := event.Text
		// Escape text for shell injection safety.
		escaped := strings.ReplaceAll(t.Text, "'", "'\\''")
		cmd := fmt.Sprintf("input text '%s'", escaped)
		_, shellErr := agt.Shell(ctx, cmd)
		if shellErr != nil {
			resp = &provider.ControlResponse{Success: false, Message: shellErr.Error()}
		} else {
			resp = &provider.ControlResponse{Success: true}
		}

	case *provider.ControlRequest_Rotate:
		r := event.Rotate
		rotationVal := 0
		switch r.Rotation {
		case 90:
			rotationVal = 1
		case 180:
			rotationVal = 2
		case 270:
			rotationVal = 3
		}
		cmd := fmt.Sprintf("settings put system accelerometer_rotation 0 && settings put system user_rotation %d", rotationVal)
		_, shellErr := agt.Shell(ctx, cmd)
		if shellErr != nil {
			resp = &provider.ControlResponse{Success: false, Message: shellErr.Error()}
		} else {
			resp = &provider.ControlResponse{Success: true}
		}

	case *provider.ControlRequest_Shell:
		sh := event.Shell
		res, shellErr := agt.ShellWithExitCode(ctx, sh.Command)
		if shellErr != nil {
			resp = &provider.ControlResponse{
				Success: false,
				Message: shellErr.Error(),
			}
		} else {
			resp = &provider.ControlResponse{
				Success: true,
				Response: &provider.ControlResponse_ShellResponse{
					ShellResponse: &provider.ShellCommandResponse{
						Output:   res.Output,
						ExitCode: int32(res.ExitCode),
					},
				},
			}
		}
	}

	return resp, nil
}
