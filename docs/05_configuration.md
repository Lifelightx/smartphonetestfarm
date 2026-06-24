# 05 — Configuration

---

## 1. Full Config Schema (`config/provider.yaml`)

```yaml
# =============================================================================
# Protean Provider Configuration
# All values can be overridden with environment variables.
# Env var pattern: PROVIDER_<SECTION>_<KEY> in UPPER_SNAKE_CASE
# Example: PROVIDER_COORDINATOR_ADDRESS=coordinator:9000
# =============================================================================

provider:
  # Unique human-readable name for this provider instance
  name: "lab-provider-01"
  # Hostname of this machine (used in coordinator registration)
  host: "provider-host.local"
  # IP of this machine (auto-detected if left empty)
  ip: ""
  # Port range allocated to per-device streams
  min_port: 7400
  max_port: 7700

coordinator:
  # gRPC address of the central Protean Coordinator
  address: "coordinator.internal:9000"
  # mTLS configuration
  tls:
    enabled: true
    ca_cert:     "/etc/stf/certs/ca.crt"
    client_cert: "/etc/stf/certs/provider.crt"
    client_key:  "/etc/stf/certs/provider.key"
  # How often to send heartbeat per device
  heartbeat_interval: "10s"
  # Maximum wait between reconnect attempts
  reconnect_max_backoff: "2m"
  # Timeout for individual gRPC calls
  call_timeout: "5s"

adb:
  # ADB daemon host (almost always localhost)
  host: "127.0.0.1"
  # ADB daemon port
  port: 5037
  # Timeout for fetching device properties
  property_timeout: "5s"
  # How often to poll for device list changes
  poll_interval: "2s"

stream:
  # Capture method: mjpeg (default) | webrtc (future)
  type: "mjpeg"
  # JPEG quality (1-100)
  quality: 80
  # Maximum frames per second
  max_fps: 15
  # Max restart attempts if stream process crashes
  max_restarts: 3

metrics:
  enabled: true
  # Port for Prometheus /metrics endpoint
  port: 9090
  path: "/metrics"

grpc_server:
  # Provider's inbound gRPC server (for coordinator callbacks)
  port: 9091
  enabled: true

logging:
  # Log level: debug | info | warn | error
  level: "info"
  # Format: json | text
  format: "json"
```

---

## 2. Environment Variable Overrides

Every config key maps to an env var using the pattern `PROVIDER_<PATH>` in `UPPER_SNAKE_CASE`.

| Config Key | Env Var |
|-----------|---------|
| `provider.name` | `PROVIDER_NAME` |
| `provider.host` | `PROVIDER_HOST` |
| `coordinator.address` | `PROVIDER_COORDINATOR_ADDRESS` |
| `coordinator.tls.enabled` | `PROVIDER_COORDINATOR_TLS_ENABLED` |
| `coordinator.tls.ca_cert` | `PROVIDER_COORDINATOR_TLS_CA_CERT` |
| `coordinator.heartbeat_interval` | `PROVIDER_COORDINATOR_HEARTBEAT_INTERVAL` |
| `adb.host` | `PROVIDER_ADB_HOST` |
| `adb.port` | `PROVIDER_ADB_PORT` |
| `stream.quality` | `PROVIDER_STREAM_QUALITY` |
| `metrics.port` | `PROVIDER_METRICS_PORT` |
| `logging.level` | `PROVIDER_LOGGING_LEVEL` |

---

## 3. Config Struct (Go)

```go
// internal/config/config.go

type Config struct {
    Provider    ProviderConfig    `mapstructure:"provider"`
    Coordinator CoordinatorConfig `mapstructure:"coordinator"`
    ADB         ADBConfig         `mapstructure:"adb"`
    Stream      StreamConfig      `mapstructure:"stream"`
    Metrics     MetricsConfig     `mapstructure:"metrics"`
    GRPCServer  GRPCServerConfig  `mapstructure:"grpc_server"`
    Logging     LoggingConfig     `mapstructure:"logging"`
}

type ProviderConfig struct {
    Name    string `mapstructure:"name"     validate:"required"`
    Host    string `mapstructure:"host"     validate:"required"`
    IP      string `mapstructure:"ip"`
    MinPort int    `mapstructure:"min_port" validate:"min=1024,max=65535"`
    MaxPort int    `mapstructure:"max_port" validate:"min=1024,max=65535"`
}

type CoordinatorConfig struct {
    Address              string        `mapstructure:"address"                validate:"required"`
    TLS                  TLSConfig     `mapstructure:"tls"`
    HeartbeatInterval    time.Duration `mapstructure:"heartbeat_interval"`
    ReconnectMaxBackoff  time.Duration `mapstructure:"reconnect_max_backoff"`
    CallTimeout          time.Duration `mapstructure:"call_timeout"`
}

type TLSConfig struct {
    Enabled    bool   `mapstructure:"enabled"`
    CACert     string `mapstructure:"ca_cert"`
    ClientCert string `mapstructure:"client_cert"`
    ClientKey  string `mapstructure:"client_key"`
}

type ADBConfig struct {
    Host            string        `mapstructure:"host"`
    Port            int           `mapstructure:"port"`
    PropertyTimeout time.Duration `mapstructure:"property_timeout"`
    PollInterval    time.Duration `mapstructure:"poll_interval"`
}

type StreamConfig struct {
    Type        string `mapstructure:"type"`
    Quality     int    `mapstructure:"quality"     validate:"min=1,max=100"`
    MaxFPS      int    `mapstructure:"max_fps"     validate:"min=1,max=60"`
    MaxRestarts int    `mapstructure:"max_restarts"`
}

type MetricsConfig struct {
    Enabled bool   `mapstructure:"enabled"`
    Port    int    `mapstructure:"port"`
    Path    string `mapstructure:"path"`
}

type GRPCServerConfig struct {
    Port    int  `mapstructure:"port"`
    Enabled bool `mapstructure:"enabled"`
}

type LoggingConfig struct {
    Level  string `mapstructure:"level"`
    Format string `mapstructure:"format"`
}
```

---

## 4. Config Loader

```go
func Load(path string) (*Config, error) {
    v := viper.New()

    // Load file
    v.SetConfigFile(path)
    if err := v.ReadInConfig(); err != nil {
        return nil, fmt.Errorf("read config: %w", err)
    }

    // Env var overrides
    v.SetEnvPrefix("PROVIDER")
    v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
    v.AutomaticEnv()

    // Defaults
    v.SetDefault("adb.host", "127.0.0.1")
    v.SetDefault("adb.port", 5037)
    v.SetDefault("metrics.port", 9090)
    v.SetDefault("logging.level", "info")
    v.SetDefault("logging.format", "json")

    // Unmarshal
    var cfg Config
    if err := v.Unmarshal(&cfg); err != nil {
        return nil, fmt.Errorf("unmarshal config: %w", err)
    }

    // Validate
    validate := validator.New()
    if err := validate.Struct(&cfg); err != nil {
        return nil, fmt.Errorf("invalid config: %w", err)
    }

    return &cfg, nil
}
```

---

## 5. CLI Flags

```
Usage: provider [flags]

Flags:
  --config string      Path to config file (default: config/provider.yaml)
  --log-level string   Override log level (debug|info|warn|error)
  --version            Print version and exit
  --help               Show this help
```
