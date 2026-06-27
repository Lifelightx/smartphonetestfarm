package supervisor

import "protean-provider/internal/domain"

// DeviceState represents the current lifecycle state of a device managed
// by this provider.
//
// State machine:
//
//	┌──────────┐  Claim  ┌─────────┐  Activate  ┌──────┐
//	│   Idle   │────────▶│ Claimed │────────────▶│ Busy │
//	└──────────┘         └─────────┘             └──────┘
//	     ▲                    │                     │
//	     │                    │ Release              │ Release
//	     └────────────────────┴─────────────────────┘
type DeviceState int

const (
	// StateIdle — device is connected, available for claiming.
	StateIdle DeviceState = iota
	// StateClaimed — a client has reserved the device but the session is not
	// fully established yet.
	StateClaimed
	// StateBusy — an active session is in progress.
	StateBusy
	// StateReleasing — graceful teardown is in progress.
	StateReleasing
)

func (s DeviceState) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateClaimed:
		return "claimed"
	case StateBusy:
		return "busy"
	case StateReleasing:
		return "releasing"
	default:
		return "unknown"
	}
}

// IsTerminal returns true if no further transitions are possible from this state.
// Currently no terminal states exist — releasing always transitions back to idle
// or the device is removed entirely on disconnect.
func (s DeviceState) IsTerminal() bool {
	return false
}

// ── Event types emitted by a DeviceSupervisor ────────────────────────────────

// SupervisorEvent is published when a device supervisor changes state.
type SupervisorEvent struct {
	Serial    string
	OldState  DeviceState
	NewState  DeviceState
	SessionID string
	Device    *domain.Device // Non-nil if telemetry/state updated
}
