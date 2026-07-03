package goios

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

// Client defines the interface to interact with the go-ios CLI.
type Client interface {
	Run(ctx context.Context, udid string, args ...string) ([]byte, error)
	RunNoUDID(ctx context.Context, args ...string) ([]byte, error)
}

// CLIClient implements Client by invoking the `ios` binary.
type CLIClient struct {
	binPath string
}

// NewClient creates a new CLIClient.
func NewClient() *CLIClient {
	return &CLIClient{
		binPath: "ios",
	}
}

// Run executes the `ios` command targeting a specific device.
func (c *CLIClient) Run(ctx context.Context, udid string, args ...string) ([]byte, error) {
	cmdArgs := append([]string{"--udid=" + udid}, args...)
	return c.execute(ctx, cmdArgs...)
}

// RunNoUDID executes the `ios` command without specifying a target device (e.g., for listing devices).
func (c *CLIClient) RunNoUDID(ctx context.Context, args ...string) ([]byte, error) {
	return c.execute(ctx, args...)
}

func (c *CLIClient) execute(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.binPath, args...)
	slog.Debug("goios: running command", "bin", c.binPath, "args", strings.Join(args, " "))

	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("goios command failed: %w (stderr: %s)", err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("goios run failed: %w", err)
	}

	return out, nil
}
