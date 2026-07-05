# Protean Provider

Protean Provider is a production-grade edge daemon written in Go that acts as a bridge between physical mobile devices (Android and iOS) and the Protean Coordinator. It allows for remote device farm management, low-latency screen streaming, and robust native device automation.

## Key Features

- **Cross-Platform Device Management**: Seamlessly tracks and manages both Android (via ADB) and iOS devices (via go-ios and WebDriverAgent).
- **High-Performance Screen Streaming**:
  - **Android**: Supports direct H.264 streaming and WebSocket broadcasting for smooth playback in the browser.
  - **iOS**: Utilizes high-performance MJPEG streaming over WebSocket, configured for optimal framerates and quality without compromising on latency.
- **Ultra Low-Latency Interactive Control**:
  - **Android**: Native touch and key event injection.
  - **iOS**: Employs the W3C Actions API via WebDriverAgent to bypass legacy element resolution, achieving near-instantaneous touch, swipe, and keyboard input response times.
- **Native Automation Engine**: Ships with a robust automation framework for executing dynamic scripts (e.g., UI interactions, assertions, app launching/termination) natively on the device without relying on heavy external wrappers like Appium.
- **gRPC Coordination**: Maintains a persistent, reliable heartbeat and bi-directional control stream with the central coordinator to handle dynamic device allocation and releases.

## Getting Started

### Prerequisites
- Go 1.21+
- ADB (for Android support)
- go-ios and WebDriverAgent (for iOS support)
- Make

### Quick Start

```bash
# 1. Clone the repository
git clone <repo>
cd protean-provider

# 2. Build the project
make build

# 3. Run the provider
make run
```

## Authentication

When authentication is enabled (default behavior unless `BYPASS_AUTH_IN_DEV=true` is set), the backend will automatically seed a default administrator user:

- **Email:** `admin@domain.com`
- **Password:** `Welcome@2026`

For API integrations or custom automation tasks, users can register and manage API keys under the auth service.

## Documentation

Full technical documentation can be found in the [`docs/`](./docs) directory:

- [Overview & BRD](./docs/01_overview_and_brd.md)
- [System Architecture](./docs/02_architecture.md)
- [Project Structure](./docs/03_project_structure.md)
- [Automation Framework](./docs/13_automation_framework.md)

See [`docs/README.md`](./docs/README.md) for the complete table of contents.
