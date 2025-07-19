package config

import (
	"time"
)

// ServerConfig represents the main server configuration
type ServerConfig struct {
	// Server settings
	Address              string
	GRPCPort             int
	RESTPort             int
	RESTEnabled          bool
	GRPCEnabled          bool
	TLSEnabled           bool
	MaxConcurrentStreams uint32
	MaxEventsPerSec      uint32
	MaxBatchSize         uint32

	// Correlation settings
	Correlation CorrelationConfig

	// Metrics settings
	Metrics MetricsConfig

	// Resource settings
	Resources ResourceConfig

	// REST API settings
	REST RESTConfig
}

// CorrelationConfig configures the correlation engine
type CorrelationConfig struct {
	Enabled             bool
	BufferSize          int
	AnalysisWindow      time.Duration
	MaxCorrelationDepth int
}

// MetricsConfig configures metrics collection
type MetricsConfig struct {
	PrometheusEnabled  bool
	PrometheusPort     int
	CollectionInterval time.Duration
}

// ResourceConfig configures resource limits
type ResourceConfig struct {
	MaxMemoryMB int
	MaxCPUMilli int
}

// RESTConfig configures REST API server
type RESTConfig struct {
	Enabled      bool          `mapstructure:"enabled"`
	Port         int           `mapstructure:"port"`
	Host         string        `mapstructure:"host"`
	EnableCORS   bool          `mapstructure:"enable_cors"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

// DefaultServerConfig returns a default server configuration
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		Address:              "0.0.0.0",
		GRPCPort:             9090,
		RESTPort:             8080,
		RESTEnabled:          true,
		GRPCEnabled:          true,
		TLSEnabled:           false,
		MaxConcurrentStreams: 1000,
		MaxEventsPerSec:      165000,
		MaxBatchSize:         1000,

		Correlation: CorrelationConfig{
			Enabled:             true,
			BufferSize:          100000,
			AnalysisWindow:      5 * time.Minute,
			MaxCorrelationDepth: 10,
		},

		Metrics: MetricsConfig{
			PrometheusEnabled:  true,
			PrometheusPort:     9091,
			CollectionInterval: 15 * time.Second,
		},

		Resources: ResourceConfig{
			MaxMemoryMB: 500,
			MaxCPUMilli: 500,
		},

		REST: RESTConfig{
			Enabled:      true,
			Port:         8080,
			Host:         "0.0.0.0",
			EnableCORS:   true,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 30 * time.Second,
		},
	}
}
