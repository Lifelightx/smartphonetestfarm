package stream

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// ScrcpyServerJarOnDevice is the destination path on the android device.
const ScrcpyServerJarOnDevice = "/data/local/tmp/scrcpy-server.jar"

// ScrcpyServerJar returns the path to the local scrcpy-server.jar file.
// It looks next to the running executable first, then falls back to the source-tree location.
func ScrcpyServerJar() (string, error) {
	// 1. Next to the binary: <exe-dir>/scrcpy-server.jar
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "scrcpy-server.jar")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	// 2. Source-tree location: internal/stream/scrcpy-server.jar
	p := filepath.Join("internal", "stream", "scrcpy-server.jar")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}

	return "", fmt.Errorf("scrcpy-server.jar not found next to binary or in internal/stream/")
}

// PushScrcpyServer pushes scrcpy-server.jar to the device and marks it readable.
func PushScrcpyServer(ctx context.Context, serial string) error {
	jarPath, err := ScrcpyServerJar()
	if err != nil {
		return fmt.Errorf("pushScrcpyServer: %w", err)
	}

	slog.Info("stream: pushing scrcpy-server to device", "serial", serial, "src", jarPath)

	out, err := exec.CommandContext(ctx, "adb", "-s", serial, "push", jarPath, ScrcpyServerJarOnDevice).CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb push: %w (out: %s)", err, out)
	}

	// Ensure the file is readable by the app on-device.
	_ = exec.CommandContext(ctx, "adb", "-s", serial, "shell", "chmod", "644", ScrcpyServerJarOnDevice).Run()
	return nil
}

func adbForward(ctx context.Context, serial string, local int, remote string) error {
	out, err := exec.CommandContext(ctx, "adb", "-s", serial,
		"forward", fmt.Sprintf("tcp:%d", local), remote,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb forward tcp:%d→%s: %w (out: %s)", local, remote, err, out)
	}
	return nil
}

func adbForwardRemove(ctx context.Context, serial string, local int) error {
	_, err := exec.CommandContext(ctx, "adb", "-s", serial,
		"forward", "--remove", fmt.Sprintf("tcp:%d", local),
	).CombinedOutput()
	return err
}
