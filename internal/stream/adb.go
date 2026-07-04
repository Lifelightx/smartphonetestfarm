package stream

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
)

//go:embed scrcpy-server.jar
var scrcpyServerJarBytes []byte

// ScrcpyServerJarOnDevice is the destination path on the android device.
const ScrcpyServerJarOnDevice = "/data/local/tmp/scrcpy-server.jar"

// PushScrcpyServer pushes scrcpy-server.jar to the device and marks it readable.
func PushScrcpyServer(ctx context.Context, serial string) error {
	slog.Info("stream: pushing scrcpy-server from embed to device", "serial", serial)

	// Create a temporary file to write the embedded bytes for adb push
	tmpFile, err := os.CreateTemp("", "scrcpy-server-*.jar")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.Write(scrcpyServerJarBytes); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	_ = tmpFile.Close() // Close before pushing so adb can read it cleanly

	out, err := exec.CommandContext(ctx, "adb", "-s", serial, "push", tmpFile.Name(), ScrcpyServerJarOnDevice).CombinedOutput()
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
