// Package goios implements native iOS communication using the go-ios library.
//
// File: syslog.go
// This file contains implementation and helper structures for native iOS communication using the go-ios library.

package goios

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
)

// SyslogStreamer handles reading device syslog lines.
type SyslogStreamer struct {
	binPath string
}

// NewSyslogStreamer creates a new SyslogStreamer.
func NewSyslogStreamer() *SyslogStreamer {
	return &SyslogStreamer{binPath: "ios"}
}

// Stream starts executing `ios syslog` and streams logs into the provided channel.
// It stops execution when the context is cancelled.
func (s *SyslogStreamer) Stream(ctx context.Context, serial string, logChan chan<- string) error {
	cmd := exec.CommandContext(ctx, s.binPath, "--udid="+serial, "syslog")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("syslog: failed to get stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("syslog: failed to start process: %w", err)
	}

	go func() {
		defer stdout.Close()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			case logChan <- scanner.Text():
			}
		}
		_ = cmd.Wait()
	}()

	return nil
}
