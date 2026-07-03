package grpc

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
				// 3 is HOME key in Android keycode, map to iOS Homescreen
				if k.KeyCode == 3 {
					err := wdaClient.Request(ctx, "POST", "/wda/homescreen", nil, nil)
					if err != nil {
						resp = &provider.ControlResponse{Success: false, Message: err.Error()}
					} else {
						resp = &provider.ControlResponse{Success: true}
					}
				} else {
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
		resp = &provider.ControlResponse{
			Success: false,
			Message: "Shell commands not supported on iOS",
		}
	}

	return resp, nil
}
