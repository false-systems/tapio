package pipeline

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// IntelligencePipeline defines the interface for event processing pipelines
type IntelligencePipeline interface {
	// ProcessEvent processes a single event through the pipeline
	ProcessEvent(event *domain.UnifiedEvent) error

	// ProcessBatch processes a batch of events through the pipeline
	ProcessBatch(events []*domain.UnifiedEvent) error

	// Start initializes and starts the pipeline
	Start(ctx context.Context) error

	// Stop gracefully shuts down the pipeline
	Stop() error

	// Shutdown is an alias for Stop to match the interface requirement
	Shutdown() error

	// GetMetrics returns current pipeline performance metrics
	GetMetrics() PipelineMetrics

	// IsRunning returns whether the pipeline is currently running
	IsRunning() bool

	// GetConfig returns the pipeline configuration
	GetConfig() PipelineConfig
}

// PipelineMode defines the operating mode of the pipeline
type PipelineMode string

const (
	// PipelineModeHighPerformance optimizes for throughput and parallelism
	PipelineModeHighPerformance PipelineMode = "high-performance"

	// PipelineModeStandard provides balanced performance with lower resource usage
	PipelineModeStandard PipelineMode = "standard"

	// PipelineModeDebug enables additional logging and validation
	PipelineModeDebug PipelineMode = "debug"
)

// PipelineConfig holds configuration for pipeline creation
type PipelineConfig struct {
	// Mode determines the pipeline implementation to use
	Mode PipelineMode

	// Orchestrator configuration
	OrchestratorConfig *OrchestratorConfig

	// Stage configuration
	EnableValidation  bool
	EnableContext     bool
	EnableCorrelation bool

	// Performance tuning
	MaxConcurrency int
	BatchSize      int
	BufferSize     int

	// Timeouts and intervals
	ProcessingTimeout time.Duration
	MetricsInterval   time.Duration
	ShutdownTimeout   time.Duration

	// Feature flags
	EnableMetrics        bool
	EnableTracing        bool
	EnableProfiling      bool
	EnableCircuitBreaker bool

	// Error handling
	MaxRetries              int
	RetryBackoff            time.Duration
	ErrorThreshold          float64
	CircuitBreakerThreshold float64
}

// DefaultPipelineConfig returns a configuration with sensible defaults
func DefaultPipelineConfig() *PipelineConfig {
	return &PipelineConfig{
		Mode:               PipelineModeHighPerformance,
		OrchestratorConfig: DefaultOrchestratorConfig(),

		// All stages enabled by default
		EnableValidation:  true,
		EnableContext:     true,
		EnableCorrelation: true,

		// Performance defaults
		MaxConcurrency: 0, // 0 means use runtime.NumCPU()
		BatchSize:      1000,
		BufferSize:     10000,

		// Timing defaults
		ProcessingTimeout: 5 * time.Second,
		MetricsInterval:   1 * time.Second,
		ShutdownTimeout:   30 * time.Second,

		// Features
		EnableMetrics:        true,
		EnableTracing:        false,
		EnableProfiling:      false,
		EnableCircuitBreaker: true,

		// Error handling
		MaxRetries:              3,
		RetryBackoff:            100 * time.Millisecond,
		ErrorThreshold:          0.1, // 10% error rate
		CircuitBreakerThreshold: 0.5, // 50% error rate triggers circuit breaker
	}
}

// StandardPipelineConfig returns configuration for standard mode
func StandardPipelineConfig() *PipelineConfig {
	config := DefaultPipelineConfig()
	config.Mode = PipelineModeStandard
	config.BatchSize = 100
	config.BufferSize = 1000
	config.MaxConcurrency = 4
	return config
}

// DebugPipelineConfig returns configuration for debug mode
func DebugPipelineConfig() *PipelineConfig {
	config := DefaultPipelineConfig()
	config.Mode = PipelineModeDebug
	config.BatchSize = 10
	config.BufferSize = 100
	config.MaxConcurrency = 1
	config.EnableTracing = true
	config.EnableProfiling = true
	return config
}

// Validate checks if the configuration is valid
func (c *PipelineConfig) Validate() error {
	if c.BatchSize <= 0 {
		c.BatchSize = 1000
	}
	if c.BufferSize <= 0 {
		c.BufferSize = 10000
	}
	if c.ProcessingTimeout <= 0 {
		c.ProcessingTimeout = 5 * time.Second
	}
	if c.MetricsInterval <= 0 {
		c.MetricsInterval = 1 * time.Second
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 30 * time.Second
	}
	if c.MaxRetries < 0 {
		c.MaxRetries = 0
	}
	if c.RetryBackoff <= 0 {
		c.RetryBackoff = 100 * time.Millisecond
	}
	if c.ErrorThreshold < 0 || c.ErrorThreshold > 1 {
		c.ErrorThreshold = 0.1
	}
	if c.CircuitBreakerThreshold < 0 || c.CircuitBreakerThreshold > 1 {
		c.CircuitBreakerThreshold = 0.5
	}

	// Ensure orchestrator config exists
	if c.OrchestratorConfig == nil {
		c.OrchestratorConfig = DefaultOrchestratorConfig()
	}

	// Sync orchestrator config with pipeline config
	c.OrchestratorConfig.BatchSize = c.BatchSize
	c.OrchestratorConfig.BufferSize = c.BufferSize
	c.OrchestratorConfig.ProcessingTimeout = c.ProcessingTimeout
	c.OrchestratorConfig.MetricsInterval = c.MetricsInterval
	c.OrchestratorConfig.CorrelationEnabled = c.EnableCorrelation

	if c.MaxConcurrency > 0 {
		c.OrchestratorConfig.WorkerCount = c.MaxConcurrency
	}

	return nil
}

// Clone creates a deep copy of the configuration
func (c *PipelineConfig) Clone() *PipelineConfig {
	clone := *c
	if c.OrchestratorConfig != nil {
		orchClone := *c.OrchestratorConfig
		clone.OrchestratorConfig = &orchClone
	}
	return &clone
}
