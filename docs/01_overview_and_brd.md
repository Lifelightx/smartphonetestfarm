# 01 — Overview & Business Requirements Document (BRD)

**Project:** `protean-provider-go`
**Language:** Go 1.21+
**Date:** 2026-06-16
**Status:** In Development

---

## 1. Executive Summary

The **Protean Provider** is a production-grade Go daemon that runs on every lab machine or CI agent where Android devices are physically connected via USB or WiFi-ADB.

It is the **edge node** of the Protean device farm platform. Its sole responsibility is to bridge physical Android hardware to the central Protean Coordinator service over a secure gRPC connection.

### What It Does

| Responsibility | Description |
|----------------|-------------|
| **Detect** | Watches ADB for device connect / disconnect events |
| **Enrich** | Fetches full device metadata (model, OS, display, battery, network) |
| **Register** | Registers itself and each device with the central Coordinator |
| **Stream** | Starts live screen capture (MJPEG) and input relay per device |
| **Heartbeat** | Sends periodic health state to Coordinator |
| **Release** | Cleanly releases devices and disconnects on shutdown |

---

## 2. Problem Statement

The existing STF (Smartphone Test Farm) Node.js provider:

- Uses **ZeroMQ** for transport — incompatible with modern gRPC tooling
- Has **no mTLS auth** between internal services
- Depends on **RethinkDB** (company shutdown 2016, community-only)
- Is built on **Node 8-era** patterns — difficult to maintain and extend
- Cannot be extended cleanly without touching a large legacy codebase

**We need a clean, production-grade Go replacement** that:
- Is a single static binary
- Speaks gRPC with mTLS
- Has structured logging, Prometheus metrics, graceful shutdown
- Is testable, lintable, and CI/CD ready from day one

---

## 3. Goals

### Functional Goals
- [ ] Detect device plug/unplug via ADB in real time
- [ ] Fetch and maintain full device metadata
- [ ] Register provider identity with Coordinator on startup
- [ ] Register each connected device with Coordinator
- [ ] Send heartbeat every N seconds per device
- [ ] Start MJPEG screen capture stream per device
- [ ] Relay touch / key input events to device via ADB
- [ ] Release all devices cleanly on SIGTERM / SIGINT
- [ ] Handle unauthorized / disconnected devices gracefully

### Non-Functional Goals
- [ ] Single static Go binary — no runtime dependencies
- [ ] mTLS mutual authentication with Coordinator
- [ ] Structured JSON logs with correlation IDs
- [ ] Prometheus `/metrics` endpoint
- [ ] Config via YAML file + environment variable overrides
- [ ] Unit test coverage ≥ 80%
- [ ] Dockerfile (multi-stage, alpine-based, < 50 MB image)
- [ ] Systemd service unit for bare-metal deployment
- [ ] GitHub Actions CI pipeline (lint + test + build on every push)

---

## 4. Non-Goals (Out of Scope for This Repo)

| Item | Owner |
|------|-------|
| Web UI / dashboard | `protean-app` (Angular frontend) |
| Device booking logic | `protean-coordinator` |
| User authentication | `protean-coordinator` / `protean-api` |
| Appium test execution | `protean-bridge` |
| iOS / Windows device support | Future scope |
| REST API for external clients | `protean-api` |

---

## 5. Stakeholders

| Role | Concern |
|------|---------|
| Dev team | Clean Go codebase, testable, documented |
| QA / test engineers | Stable device connectivity, reliable streams |
| DevOps | Single binary, Docker image, systemd unit |
| Product | Devices available and bookable in the web UI |

---

## 6. Success Criteria

1. Provider binary starts, connects to Coordinator, and registers within 5 seconds
2. Device plug event detected within 2 seconds of USB connection
3. Device metadata fully enriched within 10 seconds of connection
4. Screen stream starts within 3 seconds of device going online
5. Clean shutdown completes within 30 seconds of SIGTERM
6. Provider survives Coordinator restart — reconnects with backoff
7. All tests pass with `go test -race ./...`
8. Docker image builds successfully with `docker build`

---

## 7. On-Device Agent Strategy (Do We Need an APK?)

### Decision: No APK Required for MVP — Use scrcpy + ADB Shell

The original STF required `STFService.apk` + `minicap` + `minitouch` native binaries on every device.
**We do not need this.** Here is why and what we use instead:

### What Each Approach Provides

| Capability | STF approach | Our approach | Tool |
|------------|-------------|--------------|------|
| Screen capture | `minicap` (C++ binary pushed to device) | scrcpy server JAR (ephemeral, no install) | `scrcpy-server.jar` via ADB |
| Input injection | `minitouch` (C++ binary pushed to device) | scrcpy input channel | scrcpy subprocess |
| Device model, OS | `STFService.apk` | `adb shell getprop` | pure ADB |
| Battery info | `STFService.apk` broadcast | `adb shell dumpsys battery` | pure ADB |
| Network/WiFi info | `STFService.apk` broadcast | `adb shell dumpsys wifi` | pure ADB |
| Screen resolution | `STFService.apk` | `adb shell wm size` + `wm density` | pure ADB |

### How scrcpy Works (No Permanent Install)

```
Go Provider
  │
  ├── adb push scrcpy-server.jar /data/local/tmp/
  ├── adb shell app_process /data/local/tmp com.genymobile.scrcpy.Server
  ├── adb forward tcp:27183 localabstract:scrcpy
  └── Read MJPEG/H.264 stream from forwarded socket
      Send input events back through the same socket
```

`scrcpy-server.jar` runs in memory via `app_process` — nothing is permanently installed.
When the provider kills the process, it leaves no trace on the device.

### When You WOULD Need a Custom APK (Future Phase)

Build a lightweight `protean-agent.apk` only if you need:

| Feature | Why APK is needed |
|---------|-------------------|
| Real-time battery change events | Android `BatteryManager` broadcast receiver |
| Clipboard sync (Android 10+) | `ClipboardManager` is restricted without a foreground app |
| MediaProjection-based capture | Requires user permission dialog — needs installed app context |
| Persistent device monitoring (survives ADB disconnect) | Installed app continues running |
| Remote accessibility control | `AccessibilityService` requires installed app |

### Phased Approach

```
Phase 1–4  (Weeks 1–4):   Pure ADB shell commands for all device metadata
                           No APK, no native binary pushed to device

Phase 5    (Week 5):       scrcpy server JAR pushed ephemerally per device
                           Provider manages scrcpy subprocess lifecycle
                           Input relay via scrcpy's built-in channel

Phase 6+   (Future):       Optional lightweight protean-agent.apk
                           Only if battery events or clipboard sync is needed
                           Separate Android project, separate repo
```

### Summary

> **You do NOT need to build an APK to have a production-grade device farm provider.**
> scrcpy gives you zero-install screen streaming + input at high performance.
> ADB shell covers all device metadata needs.
> Build an APK only when you hit a specific capability wall that scrcpy + ADB cannot solve.
