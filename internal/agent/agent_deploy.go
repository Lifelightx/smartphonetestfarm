// Package agent implements device agent deployment, setup, and control.
//
// File: agent_deploy.go
// This file contains implementation and helper structures for device agent deployment, setup, and control.

package agent

import (
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
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

	// 4. Automatically enable Accessibility Service for the Agent
	slog.Info("adb: Automatically enabling Accessibility Service...", "serial", deviceSerial)
	getServicesCmd := exec.Command("adb", "-s", deviceSerial, "shell", "settings", "get", "secure", "enabled_accessibility_services")
	existingOut, err := getServicesCmd.Output()
	serviceID := "com.protean.agent/com.protean.agent.services.ProteanAccessibilityService"

	if err == nil {
		existingStr := strings.TrimSpace(string(existingOut))
		if existingStr == "null" || existingStr == "" {
			existingStr = serviceID
		} else if !strings.Contains(existingStr, "com.protean.agent") {
			existingStr = existingStr + ":" + serviceID
		}

		setServicesCmd := exec.Command("adb", "-s", deviceSerial, "shell", "settings", "put", "secure", "enabled_accessibility_services", existingStr)
		if err := setServicesCmd.Run(); err != nil {
			slog.Warn("adb: failed to enable accessibility service list", "serial", deviceSerial, "err", err)
		}
	} else {
		setServicesCmd := exec.Command("adb", "-s", deviceSerial, "shell", "settings", "put", "secure", "enabled_accessibility_services", serviceID)
		_ = setServicesCmd.Run()
	}

	enableAccessibilityCmd := exec.Command("adb", "-s", deviceSerial, "shell", "settings", "put", "secure", "accessibility_enabled", "1")
	if err := enableAccessibilityCmd.Run(); err != nil {
		slog.Warn("adb: failed to enable accessibility globally", "serial", deviceSerial, "err", err)
	}

	slog.Info("adb: ✅ Agent successfully deployed and started!", "serial", deviceSerial)
	return nil
}
