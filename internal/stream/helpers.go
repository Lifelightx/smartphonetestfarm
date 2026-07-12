// Package stream implements video streaming handlers, WS relays, and H264/FMP4 processing.
//
// File: helpers.go
// This file contains implementation and helper structures for video streaming handlers, WS relays, and H264/FMP4 processing.

package stream

import (
	"context"
	"strings"

	"protean-provider/internal/agent"
)

// getForegroundPackage returns the package name of the active foreground application via ADB.
func getForegroundPackage(ctx context.Context, agt *agent.Agent, raw bool) string {
	// Fallback 1: dumpsys window windows
	res, err := agt.Shell(ctx, "dumpsys window windows")
	if err == nil {
		if pkg := parsePackageFromDumpsys(res.Output, raw); pkg != "" {
			return pkg
		}
	}

	// Fallback 2: dumpsys activity activities
	res2, err2 := agt.Shell(ctx, "dumpsys activity activities")
	if err2 == nil {
		if pkg := parsePackageFromDumpsys(res2.Output, raw); pkg != "" {
			return pkg
		}
	}

	return ""
}

// parsePackageFromDumpsys extracts the package name from window focus / activity resume dumpsys lines.
func parsePackageFromDumpsys(output string, raw bool) string {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "mCurrentFocus") || strings.Contains(line, "mFocusedApp") || strings.Contains(line, "mResumedActivity") {
			slashIdx := strings.Index(line, "/")
			if slashIdx != -1 {
				left := line[:slashIdx]
				startIdx := -1
				for i := len(left) - 1; i >= 0; i-- {
					c := left[i]
					isPkgChar := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.'
					if !isPkgChar {
						startIdx = i + 1
						break
					}
				}
				if startIdx == -1 {
					startIdx = 0
				}
				pkg := left[startIdx:]
				pkg = strings.Trim(pkg, " \t\n\r{}()=")
				if pkg != "" && strings.Contains(pkg, ".") {
					if raw || !isSystemOrLauncher(pkg) {
						return pkg
					}
				}
			}
		}
	}
	return ""
}

// isSystemOrLauncher checks if the package is a known launcher or system UI component.
func isSystemOrLauncher(pkg string) bool {
	pkg = strings.ToLower(pkg)
	return strings.Contains(pkg, "launcher") ||
		strings.Contains(pkg, "systemui") ||
		strings.Contains(pkg, "home") ||
		pkg == "android"
}
