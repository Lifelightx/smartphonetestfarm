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

	packageName := "com.protean.agent"

	// 1. Check if the package is already installed to avoid forcefully stopping the running agent
	slog.Info("adb: Checking if Agent is already installed...", "serial", deviceSerial)
	checkCmd := exec.Command("adb", "-s", deviceSerial, "shell", "pm", "path", packageName)
	out, _ := checkCmd.Output()

	if len(out) == 0 {
		// Not installed, proceed with installation
		slog.Info("adb: Agent not found, installing...", "serial", deviceSerial)
		installCmd := exec.Command("adb", "-s", deviceSerial, "install", "-r", "-g", apkPath)
		if err := installCmd.Run(); err != nil {
			return fmt.Errorf("failed to install agent: %v", err)
		}
	} else {
		slog.Info("adb: Agent is already installed, skipping installation", "serial", deviceSerial)
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
