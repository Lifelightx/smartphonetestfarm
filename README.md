# Mobile Device Farm

A high-performance, containerized, self-hosted Mobile Device Farm for real-time remote control, screen streaming, and native test automation of physical Android and iOS devices.

---

## 1. System Architecture

The farm consists of three main components designed to run in isolated Docker containers:

1. **Central Infrastructure Stack** (`docker-compose.yml`):
   - **Database**: PostgreSQL 16 for user registry, authorization policies, and test reports.
   - **Coordinator Server**: Orchestrates device states, claims/releases, WebSocket streams, and test job scheduling.
   - **Frontend UI**: Web-based dashboard for remote screen mirroring, user login, and execution monitoring.
2. **Edge Provider Node** (`docker-compose.provider.yml`):
   - A lightweight daemon running on hosts connected to physical hardware. It tracks device attachments, captures low-latency screen streams, and injects interactive pointer/keyboard events.

---

## 2. Prerequisites

- **Docker & Docker Compose** (for running the containerized services)
- **ADB Daemon** (running on host for Android device connectivity)
- **usbmuxd** (running on host/mounted socket for iOS device connectivity)

---

## 3. Getting Started (Containerized Setup)

### Step 1: Deploy Central Infrastructure
On your primary orchestrator/management server, run the following command to boot the database, coordinator server, and frontend dashboard:

```bash
docker compose up -d --build
```
*The web dashboard is now accessible at `http://<coordinator-ip>:5173`.*

### Step 2: Deploy Edge Provider Nodes
On each machine directly connected to Android/iOS devices via USB:
1. Ensure `adb` and `usbmuxd` are running on the host.
2. Update the environment variables in `docker-compose.provider.yml` to point to the Coordinator's IP address.
3. Start the edge provider container using host network mode:

```bash
docker compose -f docker-compose.provider.yml up -d --build
```

---

## 4. Default Credentials

The system automatically initializes a default Administrator account on startup. Use these credentials to sign in to the web panel:

- **Username / Email**: `admin@domain.com`
- **Password**: `Welcome@2026`

> [!WARNING]
> It is highly recommended to change the default password in the dashboard settings panel immediately after the first login.

---

## 5. Development and Manual Build

For local development without Docker, compile and run the services manually:

```bash
# 1. Fetch dependencies and compile binaries
make build-all

# 2. Run the coordinator server
make run-coordinator

# 3. Run the edge provider daemon
make run

# 4. Start the frontend developer server
make run-frontend
```

---

## 6. Technical Documentation

Detailed guides covering internal protocols, structure, and customization can be found in the [`docs/`](./docs) folder:

- [System Architecture](./docs/02_architecture.md)
- [Project Directory Layout](./docs/03_project_structure.md)
- [Configuration Reference](./docs/05_configuration.md)
- [Native Automation Framework](./docs/13_automation_framework.md)
- [Authentication & RBAC Integration](./docs/15_auth_and_rbac_integration.md)
