package config

import "time"

// ResilienceConfig provides common resilience patterns configuration
type ResilienceConfig struct {
	BaseConfig `yaml:",inline" json:",inline"`

	// Circuit breaker
	CircuitBreaker CircuitBreakerConfig `yaml:"circuit_breaker" json:"circuit_breaker"`

	// Load shedding
	LoadShedding LoadSheddingConfig `yaml:"load_shedding" json:"load_shedding"`

	// Timeout management
	Timeout TimeoutConfig `yaml:"timeout" json:"timeout"`

	// Bulkhead isolation
	Bulkhead BulkheadConfig `yaml:"bulkhead" json:"bulkhead"`

	// Health checks
	HealthCheck HealthCheckConfig `yaml:"health_check" json:"health_check"`
}

// CircuitBreakerConfig defines circuit breaker settings
type CircuitBreakerConfig struct {
	Enabled             bool          `yaml:"enabled" json:"enabled"`
	FailureThreshold    int           `yaml:"failure_threshold" json:"failure_threshold"`
	SuccessThreshold    int           `yaml:"success_threshold" json:"success_threshold"`
	Timeout             time.Duration `yaml:"timeout" json:"timeout"`
	HalfOpenMaxRequests int           `yaml:"half_open_max_requests" json:"half_open_max_requests"`
	ObservationWindow   time.Duration `yaml:"observation_window" json:"observation_window"`
}

// LoadSheddingConfig defines load shedding settings
type LoadSheddingConfig struct {
	Enabled            bool                  `yaml:"enabled" json:"enabled"`
	Strategy           string                `yaml:"strategy" json:"strategy"` // adaptive, threshold, priority
	CPUThreshold       float64               `yaml:"cpu_threshold" json:"cpu_threshold"`
	MemoryThreshold    float64               `yaml:"memory_threshold" json:"memory_threshold"`
	LatencyThreshold   time.Duration         `yaml:"latency_threshold" json:"latency_threshold"`
	ErrorRateThreshold float64               `yaml:"error_rate_threshold" json:"error_rate_threshold"`
	PriorityLevels     []string              `yaml:"priority_levels" json:"priority_levels"`
	Adaptive           AdaptiveSheddingConfig `yaml:"adaptive" json:"adaptive"`
}

// AdaptiveSheddingConfig defines adaptive shedding settings
type AdaptiveSheddingConfig struct {
	Enabled      bool          `yaml:"enabled" json:"enabled"`
	Window       time.Duration `yaml:"window" json:"window"`
	LearningRate float64       `yaml:"learning_rate" json:"learning_rate"`
	MinSamples   int           `yaml:"min_samples" json:"min_samples"`
}

// TimeoutConfig defines timeout settings
type TimeoutConfig struct {
	Default    time.Duration            `yaml:"default" json:"default"`
	PerMethod  map[string]time.Duration `yaml:"per_method" json:"per_method"`
	Connect    time.Duration            `yaml:"connect" json:"connect"`
	Read       time.Duration            `yaml:"read" json:"read"`
	Write      time.Duration            `yaml:"write" json:"write"`
	Idle       time.Duration            `yaml:"idle" json:"idle"`
	KeepAlive  time.Duration            `yaml:"keep_alive" json:"keep_alive"`
	Adaptive   bool                     `yaml:"adaptive" json:"adaptive"`
	Multiplier float64                  `yaml:"multiplier" json:"multiplier"`
}

// BulkheadConfig defines bulkhead isolation settings
type BulkheadConfig struct {
	Enabled          bool `yaml:"enabled" json:"enabled"`
	MaxConcurrency   int  `yaml:"max_concurrency" json:"max_concurrency"`
	MaxQueueSize     int  `yaml:"max_queue_size" json:"max_queue_size"`
	QueueTimeout     time.Duration `yaml:"queue_timeout" json:"queue_timeout"`
	KeepAliveTime    time.Duration `yaml:"keep_alive_time" json:"keep_alive_time"`
	CorePoolSize     int  `yaml:"core_pool_size" json:"core_pool_size"`
	MaxPoolSize      int  `yaml:"max_pool_size" json:"max_pool_size"`
}

// HealthCheckConfig defines health check settings
type HealthCheckConfig struct {
	Enabled         bool              `yaml:"enabled" json:"enabled"`
	Interval        time.Duration     `yaml:"interval" json:"interval"`
	Timeout         time.Duration     `yaml:"timeout" json:"timeout"`
	FailureThreshold int              `yaml:"failure_threshold" json:"failure_threshold"`
	SuccessThreshold int              `yaml:"success_threshold" json:"success_threshold"`
	DeepCheck       bool              `yaml:"deep_check" json:"deep_check"`
	Endpoints       []HealthEndpoint  `yaml:"endpoints" json:"endpoints"`
}

// HealthEndpoint defines a health check endpoint
type HealthEndpoint struct {
	Name     string            `yaml:"name" json:"name"`
	URL      string            `yaml:"url" json:"url"`
	Method   string            `yaml:"method" json:"method"`
	Headers  map[string]string `yaml:"headers" json:"headers"`
	Timeout  time.Duration     `yaml:"timeout" json:"timeout"`
	Critical bool              `yaml:"critical" json:"critical"`
}

// DefaultResilienceConfig returns default resilience configuration
func DefaultResilienceConfig() ResilienceConfig {
	return ResilienceConfig{
		BaseConfig: DefaultBaseConfig(),
		CircuitBreaker: CircuitBreakerConfig{
			Enabled:             true,
			FailureThreshold:    5,
			SuccessThreshold:    2,
			Timeout:             60 * time.Second,
			HalfOpenMaxRequests: 3,
			ObservationWindow:   10 * time.Second,
		},
		LoadShedding: LoadSheddingConfig{
			Enabled:            false,
			Strategy:           "threshold",
			CPUThreshold:       80.0,
			MemoryThreshold:    85.0,
			LatencyThreshold:   5 * time.Second,
			ErrorRateThreshold: 0.5,
		},
		Timeout: TimeoutConfig{
			Default:   30 * time.Second,
			Connect:   10 * time.Second,
			Read:      30 * time.Second,
			Write:     30 * time.Second,
			Idle:      60 * time.Second,
			KeepAlive: 30 * time.Second,
		},
		Bulkhead: BulkheadConfig{
			Enabled:        true,
			MaxConcurrency: 100,
			MaxQueueSize:   1000,
			QueueTimeout:   30 * time.Second,
		},
		HealthCheck: HealthCheckConfig{
			Enabled:          true,
			Interval:         30 * time.Second,
			Timeout:          5 * time.Second,
			FailureThreshold: 3,
			SuccessThreshold: 1,
		},
	}
}