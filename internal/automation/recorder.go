package automation

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"protean-provider/internal/domain"
)

// ------------------------------------------------
// Raw Event Types
// ------------------------------------------------

// RawEvent represents a single raw user interaction event captured during recording.
// No intelligence here — just facts.
type RawEvent struct {
	Time         time.Time
	Type         string  // "click", "input", "swipe", "launch"
	Package      string  // launch only
	ScreenWidth  int32
	ScreenHeight int32
	TouchX       float64
	TouchY       float64
	EndX         float64 // swipe only
	EndY         float64 // swipe only
	DurationMs   int     // swipe only
	Text         string  // input only
	UIXML        string  // full hierarchy snapshot at event time
}

// ------------------------------------------------
// Recording Session (stores raw events)
// ------------------------------------------------

// RecordingSession stores state for an active recording session.
type RecordingSession struct {
	Serial    string
	StartTime time.Time
	Events    []RawEvent
	mu        sync.Mutex
}

// RecorderManager coordinates and keeps track of active device recording sessions.
type RecorderManager struct {
	mu       sync.Mutex
	sessions map[string]*RecordingSession
}

// NewRecorderManager creates a new instance of RecorderManager.
func NewRecorderManager() *RecorderManager {
	return &RecorderManager{
		sessions: make(map[string]*RecordingSession),
	}
}

// StartRecording starts a new recording session for the specified device serial.
func (m *RecorderManager) StartRecording(serial string, launchPackage string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	events := make([]RawEvent, 0)
	if launchPackage != "" {
		events = append(events, RawEvent{
			Time:    time.Now(),
			Type:    "launch",
			Package: launchPackage,
		})
	}

	m.sessions[serial] = &RecordingSession{
		Serial:    serial,
		StartTime: time.Now(),
		Events:    events,
	}
}

// StopRecording terminates the recording session and returns raw events.
// The caller is responsible for compiling these into a Script.
func (m *RecorderManager) StopRecording(serial string) ([]RawEvent, error) {
	m.mu.Lock()
	session, ok := m.sessions[serial]
	if ok {
		delete(m.sessions, serial)
	}
	m.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("no active recording session for device %s", serial)
	}

	session.mu.Lock()
	events := session.Events
	session.mu.Unlock()

	return events, nil
}

// IsRecording returns true if the specified device is currently recording.
func (m *RecorderManager) IsRecording(serial string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[serial]
	return ok
}

// RecordClick captures a raw click event. The recorder is stupid — it just saves
// the coordinates, screen size, and a full UI hierarchy dump. No XPath, no locators.
func (m *RecorderManager) RecordClick(ctx context.Context, serial string, driver domain.Driver, x, y float64) error {
	m.mu.Lock()
	session, ok := m.sessions[serial]
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("no active recording session for device %s", serial)
	}

	xmlData, err := driver.DumpUI(ctx)
	if err != nil {
		slog.Warn("recorder: failed to dump UI tree, recording without XML", "serial", serial, "err", err)
		xmlData = ""
	}

	width, height, err := driver.ScreenSize(ctx)
	if err != nil {
		slog.Warn("recorder: failed to get screen size", "serial", serial, "err", err)
		width, height = 1080, 1920 // reasonable defaults
	}

	event := RawEvent{
		Time:         time.Now(),
		Type:         "click",
		ScreenWidth:  width,
		ScreenHeight: height,
		TouchX:       x,
		TouchY:       y,
		UIXML:        xmlData,
	}

	slog.Info("recorder: captured click event", "serial", serial, "x", x, "y", y, "hasXML", xmlData != "")

	session.mu.Lock()
	session.Events = append(session.Events, event)
	session.mu.Unlock()

	return nil
}

// RecordTextInput appends a raw text input event.
func (m *RecorderManager) RecordTextInput(serial string, text string) error {
	m.mu.Lock()
	session, ok := m.sessions[serial]
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("no active recording session for device %s", serial)
	}

	session.mu.Lock()
	session.Events = append(session.Events, RawEvent{
		Time: time.Now(),
		Type: "input",
		Text: text,
	})
	session.mu.Unlock()

	return nil
}

// RecordSwipe appends a raw swipe event.
func (m *RecorderManager) RecordSwipe(serial string, startX, startY, endX, endY float64, durationMs int) error {
	m.mu.Lock()
	session, ok := m.sessions[serial]
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("no active recording session for device %s", serial)
	}

	session.mu.Lock()
	session.Events = append(session.Events, RawEvent{
		Time:       time.Now(),
		Type:       "swipe",
		TouchX:     startX,
		TouchY:     startY,
		EndX:       endX,
		EndY:       endY,
		DurationMs: durationMs,
	})
	session.mu.Unlock()

	return nil
}
