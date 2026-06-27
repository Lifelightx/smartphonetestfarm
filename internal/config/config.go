package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/spf13/viper"
)

// Config is the top-level configuration for the provider binary.
type Config struct {
	Provider    ProviderConfig    `mapstructure:"provider"`
	Coordinator CoordinatorConfig `mapstructure:"coordinator"`
	ADB         ADBConfig         `mapstructure:"adb"`
	Stream      StreamConfig      `mapstructure:"stream"`
	Metrics     MetricsConfig     `mapstructure:"metrics"`
	GRPCServer  GRPCServerConfig  `mapstructure:"grpc_server"`
	Logging     LoggingConfig     `mapstructure:"logging"`
}

// ProviderConfig describes this provider instance.
type ProviderConfig struct {
	Name    string `mapstructure:"name"     validate:"required"`
	Host    string `mapstructure:"host"     validate:"required"`
	IP      string `mapstructure:"ip"`
	MinPort int    `mapstructure:"min_port" validate:"min=1024,max=65535"`
	MaxPort int    `mapstructure:"max_port" validate:"min=1024,max=65535"`
}

// CoordinatorConfig describes the remote coordinator gRPC endpoint.
type CoordinatorConfig struct {
	Address             string        `mapstructure:"address"                validate:"required"`
	TLS                 TLSConfig     `mapstructure:"tls"`
	HeartbeatInterval   time.Duration `mapstructure:"heartbeat_interval"`
	ReconnectMaxBackoff time.Duration `mapstructure:"reconnect_max_backoff"`
	CallTimeout         time.Duration `mapstructure:"call_timeout"`
}

// TLSConfig holds mTLS certificate paths.
type TLSConfig struct {
	Enabled    bool   `mapstructure:"enabled"`
	CACert     string `mapstructure:"ca_cert"`
	ClientCert string `mapstructure:"client_cert"`
	ClientKey  string `mapstructure:"client_key"`
}

// ADBConfig describes the local ADB daemon connection.
type ADBConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	PropertyTimeout time.Duration `mapstructure:"property_timeout"`
}

// StreamConfig controls the screen-capture subsystem.
type StreamConfig struct {
	Type        string `mapstructure:"type"`
	Quality     int    `mapstructure:"quality"     validate:"min=1,max=100"`
	MaxFPS      int    `mapstructure:"max_fps"     validate:"min=1,max=60"`
	MaxRestarts int    `mapstructure:"max_restarts"`
}

// MetricsConfig controls the Prometheus metrics HTTP endpoint.
type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Port    int    `mapstructure:"port"`
	Path    string `mapstructure:"path"`
}

// GRPCServerConfig controls the provider's inbound gRPC server.
type GRPCServerConfig struct {
	Port    int  `mapstructure:"port"`
	Enabled bool `mapstructure:"enabled"`
}

// LoggingConfig controls structured logging.
type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// Load reads the YAML config from path, applies environment variable overrides,
// sets defaults, and validates required fields.
//
// Environment variable pattern:  PROVIDER_<SECTION>_<KEY>  (UPPER_SNAKE_CASE)
// Example: PROVIDER_COORDINATOR_ADDRESS=coordinator:9000
func Load(path string) (*Config, error) {
	v := viper.New()

	// ── File ──────────────────────────────────────────────────────────────────
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("config: read file %q: %w", path, err)
	}

	// ── Environment overrides ─────────────────────────────────────────────────
	v.SetEnvPrefix("PROVIDER")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// ── Defaults ──────────────────────────────────────────────────────────────
	v.SetDefault("adb.host", "127.0.0.1")
	v.SetDefault("adb.port", 5037)
	v.SetDefault("adb.property_timeout", "5s")
	v.SetDefault("coordinator.heartbeat_interval", "10s")
	v.SetDefault("coordinator.reconnect_max_backoff", "2m")
	v.SetDefault("coordinator.call_timeout", "5s")
	v.SetDefault("stream.type", "mjpeg")
	v.SetDefault("stream.quality", 80)
	v.SetDefault("stream.max_fps", 15)
	v.SetDefault("stream.max_restarts", 3)
	v.SetDefault("metrics.enabled", true)
	v.SetDefault("metrics.port", 9090)
	v.SetDefault("metrics.path", "/metrics")
	v.SetDefault("grpc_server.port", 9091)
	v.SetDefault("grpc_server.enabled", true)
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "json")
	v.SetDefault("provider.min_port", 7400)
	v.SetDefault("provider.max_port", 7700)

	// ── Unmarshal ─────────────────────────────────────────────────────────────
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}

	// ── Validate ──────────────────────────────────────────────────────────────
	validate := validator.New()
	if err := validate.Struct(&cfg); err != nil {
		return nil, fmt.Errorf("config: validation: %w", err)
	}

	return &cfg, nil
}
