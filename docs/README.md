# protean-provider-go — Documentation

This folder contains the full technical documentation for building the **Protean Provider** in Go — a production-grade edge daemon that bridges physical Android devices to the Protean Coordinator over gRPC.

---

## Documents

| File | What it covers |
|------|---------------|
| [01_overview_and_brd.md](./01_overview_and_brd.md) | Business Requirements Document — what, why, goals, non-goals |
| [02_architecture.md](./02_architecture.md) | Full system architecture, component diagram, data flow |
| [03_project_structure.md](./03_project_structure.md) | Every file and folder explained |
| [04_domain_and_interfaces.md](./04_domain_and_interfaces.md) | Domain models, interfaces, state machine |
| [05_configuration.md](./05_configuration.md) | Full config schema, env vars, validation |
| [06_grpc_and_proto.md](./06_grpc_and_proto.md) | Protobuf contracts, gRPC services, code generation |
| [07_implementation_phases.md](./07_implementation_phases.md) | Step-by-step build plan, week-by-week checklist |
| [08_testing_strategy.md](./08_testing_strategy.md) | Unit, integration, E2E testing approach |
| [09_observability.md](./09_observability.md) | Logging, metrics, health checks |
| [10_deployment.md](./10_deployment.md) | Dockerfile, systemd, CI/CD pipeline |
| [11_error_handling.md](./11_error_handling.md) | Error scenarios and recovery strategies |
| [12_dependencies.md](./12_dependencies.md) | All Go packages with justification |

---

## Quick Start for Developers

```bash
# 1. Clone
git clone <repo>
cd protean-provider-go

# 2. Install tools
make install-tools

# 3. Generate proto
make proto

# 4. Build
make build

# 5. Run locally
make run
```

See [07_implementation_phases.md](./07_implementation_phases.md) for what to build next.
