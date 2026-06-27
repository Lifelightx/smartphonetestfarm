package adb

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
)

// DeployAgent installs the APK and starts the background service with the correct ADB serial and port
func DeployAgent(deviceSerial string, port int) error {
	apkPath := "assets/protean-agent.apk"

	// 1. Install the APK (with -g to auto-grant all permissions)
	slog.Info("adb: Installing Agent...", "serial", deviceSerial)
	installCmd := exec.Command("adb", "-s", deviceSerial, "install", "-r", "-g", apkPath)
	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("failed to install agent: %v", err)
	}

	// 2. Set up the Reverse TCP Tunnel so the agent can talk to your Go Provider on localhost
	slog.Info("adb: Setting up reverse tunnel...", "serial", deviceSerial, "port", port)
	reverseCmd := exec.Command("adb", "-s", deviceSerial, "reverse", fmt.Sprintf("tcp:%d", port), fmt.Sprintf("tcp:%d", port))
	if err := reverseCmd.Run(); err != nil {
		return fmt.Errorf("failed to setup adb reverse: %v", err)
	}

	// 3. Start the AgentService silently in the background and inject the Serial ID and Port
	slog.Info("adb: Starting Agent Service...", "serial", deviceSerial, "port", port)
	startCmd := exec.Command("adb", "-s", deviceSerial, "shell", "am", "start-foreground-service",
		"-a", "com.protean.agent.START",
		"-e", "serial", deviceSerial,
		"-e", "port", strconv.Itoa(port))

	if err := startCmd.Run(); err != nil {
		return fmt.Errorf("failed to start agent service: %v", err)
	}

	slog.Info("adb: ✅ Agent successfully deployed and started!", "serial", deviceSerial)
	return nil
}
