# 13 — Go-STF Native Automation Framework

This document outlines the architecture and implementation details of the Go-STF Native Automation Framework (No Appium Dependency).

---

## 1. Vision & Core Principles

The native automation framework transforms Go-STF from a remote device management system into a comprehensive mobile device platform. 

### Key Characteristics:
* **Zero Appium Dependency:** Bypasses Appium's heavy node server infrastructure and startup delays by communicating directly with low-level device components.
* **Thin Agent Architecture:** On-device agents expose only basic capabilities (Tap, Swipe, Input Text, Screenshot, UI Hierarchy Dumps, App Lifecycle). They remain completely stateless with respect to automation scripts.
* **Intelligent Server-Side Engine:** All locator resolution, wait strategies, conditions, retries, test execution flow, and AI-assisted self-healing logic reside in the Go Provider/Server.

---

## 2. High-Level Architecture

```
                    Frontend / Client
                        │
                        ▼
                  Coordinator
                        │
        ┌───────────────┴───────────────┐
        │                               │
   Device Provider               Automation Service
        │                               │
        │                     Script Runner
        │                     Wait Engine
        │                     Locator Engine
        │                     Report Engine
        │
        ▼
     Android Agent (Thin)
        │
        ▼
     Android Device
```

---

## 3. Platform Abstraction & Driver Layer

The Driver layer abstracts platform-specific automation commands. The platform interface (`Driver`) is defined in `internal/domain/automation.go` and is decoupled from execution engines:

```go
type Driver interface {
    Launch(ctx context.Context, appID string) error
    Terminate(ctx context.Context, appID string) error
    Tap(ctx context.Context, x, y float64) error
    Swipe(ctx context.Context, startX, startY, endX, endY float64, durationMs int) error
    Input(ctx context.Context, text string) error
    Screenshot(ctx context.Context) ([]byte, error)
    DumpUI(ctx context.Context) (string, error)
    CurrentApp(ctx context.Context) (*AppInfo, error)
    Install(ctx context.Context, filepath string) error
    Uninstall(ctx context.Context, appID string) error
}
```

---

## 4. Completed Phase 1 Implementation

Phase 1 provides complete platform abstraction and an `AndroidDriver` implementation using native, direct ADB commands.

### Implemented Capabilities:

1. **AndroidDriver (`internal/automation/android_driver.go`):**
   * **Launch App:** Starts applications utilizing the `monkey` launcher tool (`adb shell monkey -p <app_id> -c android.intent.category.LAUNCHER 1`).
   * **Terminate App:** Force-stops packages via `am force-stop`.
   * **Coordinates Scaling & Tap:** Takes normalized coordinates `[0, 1]` for cross-device consistency, translates them dynamically via `wm size` to physical pixels, and taps (`adb shell input tap X Y`).
   * **Swipe:** Scales normalized start and end coordinates and issues `adb shell input swipe X1 Y1 X2 Y2 [duration]`.
   * **Text Input:** Injects keyboard characters directly into the focused element, properly escaping spaces (`%s`) and shell-sensitive metacharacters.
   * **Screenshot:** Executes raw binary image capture via `exec-out screencap -p` avoiding shell-based string conversion overhead.
   * **UI Dump:** Captures the current window hierarchy layout XML file via `uiautomator dump`, retrieves its contents, and automatically cleans up the remote device state.
   * **Current App:** Inspects the active foreground package and activity via `dumpsys window windows` and falls back to `dumpsys activity activities` with flexible regex parsers.
   * **Install & Uninstall:** Native APK installation (`adb install -r -g`) and package uninstallation.

2. **Unit Tests (`internal/automation/android_driver_test.go`):**
   * Employs a custom `runCmd` function hook to intercept and mock `exec.Cmd` shell executions.
   * Fully tests all driver commands, bounds scaling, regex matching, and fallback routines in isolation without running live ADB daemons or connecting hardware.

---

---

## 5. Completed Phase 2 Implementation

Phase 2 builds the core execution, locator, recording, and storage capabilities.

### Implemented Capabilities:

1. **Script DSL (`internal/automation/dsl.go`):**
   * Defined structures and parsers using `gopkg.in/yaml.v3` to process structured automation test scripts.
   * Supports `launch`, `terminate`, `click`, `input`, and `swipe` parameters.

2. **Locator Engine (`internal/automation/locator.go`):**
   * Uses Go `encoding/xml` to serialize the XML UI layout dump into a tree of `XMLNode` nodes under a `UIHierarchy` root.
   * Resolves query selectors recursively.
   * Supports Resource ID, Content Description/Accessibility ID, Text, and Class matching.
   * Parses layout bounds strings (`[left,top][right,bottom]`) and returns normalized center screen coordinates relative to physical screen size.

3. **Execution Runner (`internal/automation/runner.go`):**
   * Orchestrates step-by-step runner execution on top of the abstract `domain.Driver`.
   * Automatically captures raw screenshots on failure for step-level diagnostics.
   * Produces detailed JSON execution report metrics (`StartTime`, `EndTime`, `DurationMs`, `TotalSteps`, `PassedSteps`, and detailed lists of step-level errors and duration).

4. **Interaction Recorder (`internal/automation/recorder.go`):**
   * Captures screen pointer inputs, retrieves device UI hierarchy XML tree dumps, and resolves raw click coordinates to the deepest matching `XMLNode` in the tree.
   * Automatically derives the highest priority locator identifier (Resource ID, Content Description, Text, or Class) for recorded events to construct YAML steps.
   * Tracks active recording sessions and records keyboard inputs and swipes.

5. **Storage System (`internal/coordinator_server/db.go`):**
   * Added PostgreSQL schema migrations for `automation_scripts` and `automation_reports`.
   * Added database CRUD methods: `SaveScript`, `GetScript`, `SaveReport`, and `GetReport`.

6. **Unit Tests (`internal/automation/automation_test.go`):**
   * Validates XML deserialization, locator search precedence, YAML parsing, execution report capturing, and point-in-polygon containment resolution.

---

## 6. Completed Phase 3 Implementation

Phase 3 introduces advanced test stability features to handle real-world latency, UI transitions, and transient failures.

### Implemented Capabilities:

1. **Implicit Wait Resolution:**
   * Embedded an automatic polling mechanism inside `handleClick` targeting UI elements. The runner automatically queries and retries locating elements up to a 5-second default threshold before raising a failure.

2. **Explicit Wait Steps (`WaitParams`):**
   * Support for `wait` DSL steps with custom `TimeoutMs` values.
   * Handles check conditions for `visible` (element is active in hierarchy XML), `present`, and `hidden` (element is absent or removed).

3. **Assertions (`AssertParams`):**
   * Support for `assert` DSL steps checking condition values.
   * Handles text checking modes: `equals` (strict string match) and `contains` (substring comparison).
   * Validates visibility states (`visible`/`present`/`hidden`).

4. **Self-Healing Step Retries:**
   * Configured the runner to execute a retry loop (up to 2 retries with a 1-second backoff delay) for non-assertion step execution errors (e.g. adb socket/hardware failures) to self-heal temporary UI lag.

5. **Extended Unit Testing:**
   * Added `TestWaitAndAssertions` to verify dynamic XML updates and wait polling.
   * Added `TestStepRetries` simulating temporary command failure and recovery.

---

## 7. Next Steps (Roadmap)

### Phase 4: Parallel Test Execution & API Layer
* **Parallel Test Runner**: Implement a thread-safe scheduler to orchestrate test script replays concurrently across multiple physical or virtual device serials.
* **REST/gRPC Trigger Endpoints**: Add API endpoints to upload scripts, list reports, and inspect failures from the STF Dashboard.

### Phase 5: iOS Integration & Advanced Locators
* **WDA Client Integration**: Build `IOSDriver` implementing `domain.Driver` mapping actions directly to Xcode's WebDriverAgent HTTP API.
* **WebStream Canvas Recorder**: Connect the pointer coordinates captured on the web dashboard to the active `RecorderManager` to generate scripts dynamically.


