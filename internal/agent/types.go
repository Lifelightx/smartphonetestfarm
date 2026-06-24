package agent

import "errors"

// ErrAgentStopped is returned when an operation is attempted on a stopped agent.
var ErrAgentStopped = errors.New("agent: already stopped")

// ErrForwardNotSet is returned when a port forward is expected but not established.
var ErrForwardNotSet = errors.New("agent: port forward not established")

// CommandResult holds the output of a single ADB shell command.
type CommandResult struct {
	Output string
	// ExitCode is the exit code of the command on the device.
	// -1 means the exit code could not be determined.
	ExitCode int
}

// ForwardRule describes a single TCP port-forward rule.
// e.g. local port 7400 → remote port 8080 on the device.
type ForwardRule struct {
	LocalPort  int
	RemotePort int
	Serial     string
}
