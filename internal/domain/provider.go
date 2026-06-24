package domain

// Provider represents this running instance of the STF provider.
type Provider struct {
	// ID is a globally unique identifier (UUID v4) generated on startup.
	ID string

	// Name is a human-readable label from config (e.g. "lab-provider-01").
	Name string

	// Host is the hostname of this machine.
	Host string

	// IP is the reachable IP address of this machine.
	IP string

	// Version is the binary version string (set at build time).
	Version string

	// MinPort and MaxPort bound the per-device port allocation range.
	MinPort int
	MaxPort int
}