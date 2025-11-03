package runtime

import (
	"fmt"
	"time"
)

// Config holds the configuration for an ObserverRuntime instance.
type Config struct {
	// Name of the observer (e.g., "network", "node", "deployments")
	Name string

	// Sampling configuration
	Sampling SamplingConfig

	// Backpressure configuration
	Backpressure BackpressureConfig

	// Health check configuration
	Health HealthConfig

	// Failure handling configuration
	Failure FailureConfig

	// Metrics configuration
	Metrics MetricsConfig
}

// SamplingConfig defines event sampling behavior.
type SamplingConfig struct {
	// Enabled turns sampling on/off
	Enabled bool

	// DefaultRate is the default sample rate (0.0 - 1.0)
	// 0.1 = 10% sampling, 1.0 = keep all events
	DefaultRate float64

	// Rules define type-specific sampling overrides
	Rules []SamplingRule
}

// SamplingRule defines sampling for a specific event type.
type SamplingRule struct {
	// EventType to match (e.g., "network", "node")
	EventType string

	// Subtype to match (e.g., "dns_query", "link_failure")
	// Empty string matches all subtypes
	Subtype string

	// Rate override for this type (0.0 - 1.0)
	// Ignored if KeepAll is true
	Rate float64

	// KeepAll keeps all events of this type (100% sampling)
	// Use for critical events like failures
	KeepAll bool
}

// Matches returns true if this rule matches the given event type/subtype.
func (r SamplingRule) Matches(eventType, subtype string) bool {
	if r.EventType != eventType {
		return false
	}
	if r.Subtype != "" && r.Subtype != subtype {
		return false
	}
	return true
}

// BackpressureConfig defines queue and backpressure behavior.
type BackpressureConfig struct {
	// QueueSize is the maximum number of events to buffer
	QueueSize int

	// DropPolicy defines what to drop when queue is full
	DropPolicy DropPolicy
}

// DropPolicy defines what to do when queue is full.
type DropPolicy string

const (
	// DropOldest drops oldest events first (keep recent)
	DropOldest DropPolicy = "oldest"

	// DropNewest drops incoming events (preserve history)
	DropNewest DropPolicy = "newest"

	// DropRandom drops random events
	DropRandom DropPolicy = "random"
)

// HealthConfig defines health checking behavior.
type HealthConfig struct {
	// Enabled turns health checking on/off
	Enabled bool

	// CheckInterval is how often to run health checks
	CheckInterval time.Duration
}

// FailureConfig defines failure handling and retry behavior.
type FailureConfig struct {
	// Policy defines how to handle failures
	Policy FailurePolicy

	// Retry configuration
	Retry RetryConfig
}

// FailurePolicy defines failure handling behavior.
type FailurePolicy string

const (
	// FailPolicyIsolate continues other observers if this one fails
	// Recommended for production (graceful degradation)
	FailPolicyIsolate FailurePolicy = "isolate"

	// FailPolicyRestart retries with exponential backoff
	FailPolicyRestart FailurePolicy = "restart"

	// FailPolicyFailFast crashes entire binary on first failure
	// Use for critical observers that must work
	FailPolicyFailFast FailurePolicy = "fail_fast"
)

// RetryConfig defines retry behavior for FailPolicyRestart.
type RetryConfig struct {
	// MaxAttempts before giving up
	MaxAttempts int

	// InitialDelay before first retry
	InitialDelay time.Duration

	// MaxDelay caps the retry delay
	MaxDelay time.Duration

	// Multiplier for exponential backoff
	Multiplier float64
}

// MetricsConfig defines metrics collection behavior.
type MetricsConfig struct {
	// Enabled turns metrics collection on/off
	Enabled bool

	// AllowedLabels defines which labels to include in metrics
	// Use this to control cardinality
	// Example: ["namespace", "observer_type"] but NOT ["pod_name", "container_id"]
	AllowedLabels []string
}

// Validate checks if the config is valid.
func (c *Config) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("config.Name is required")
	}

	if c.Sampling.Enabled {
		if c.Sampling.DefaultRate < 0.0 || c.Sampling.DefaultRate > 1.0 {
			return fmt.Errorf("config.Sampling.DefaultRate must be between 0.0 and 1.0, got %f", c.Sampling.DefaultRate)
		}

		for i, rule := range c.Sampling.Rules {
			if rule.EventType == "" {
				return fmt.Errorf("config.Sampling.Rules[%d].EventType is required", i)
			}
			if !rule.KeepAll && (rule.Rate < 0.0 || rule.Rate > 1.0) {
				return fmt.Errorf("config.Sampling.Rules[%d].Rate must be between 0.0 and 1.0, got %f", i, rule.Rate)
			}
		}
	}

	if c.Backpressure.QueueSize <= 0 {
		return fmt.Errorf("config.Backpressure.QueueSize must be > 0, got %d", c.Backpressure.QueueSize)
	}

	if c.Backpressure.DropPolicy != DropOldest &&
		c.Backpressure.DropPolicy != DropNewest &&
		c.Backpressure.DropPolicy != DropRandom {
		return fmt.Errorf("config.Backpressure.DropPolicy must be 'oldest', 'newest', or 'random', got %s", c.Backpressure.DropPolicy)
	}

	if c.Health.Enabled && c.Health.CheckInterval <= 0 {
		return fmt.Errorf("config.Health.CheckInterval must be > 0 when health checks enabled")
	}

	if c.Failure.Policy != FailPolicyIsolate &&
		c.Failure.Policy != FailPolicyRestart &&
		c.Failure.Policy != FailPolicyFailFast {
		return fmt.Errorf("config.Failure.Policy must be 'isolate', 'restart', or 'fail_fast', got %s", c.Failure.Policy)
	}

	if c.Failure.Policy == FailPolicyRestart {
		if c.Failure.Retry.MaxAttempts <= 0 {
			return fmt.Errorf("config.Failure.Retry.MaxAttempts must be > 0 for restart policy")
		}
		if c.Failure.Retry.InitialDelay <= 0 {
			return fmt.Errorf("config.Failure.Retry.InitialDelay must be > 0 for restart policy")
		}
		if c.Failure.Retry.Multiplier <= 1.0 {
			return fmt.Errorf("config.Failure.Retry.Multiplier must be > 1.0 for exponential backoff")
		}
	}

	return nil
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig(name string) Config {
	return Config{
		Name: name,
		Sampling: SamplingConfig{
			Enabled:     true,
			DefaultRate: 0.1, // 10% sampling by default
			Rules:       []SamplingRule{},
		},
		Backpressure: BackpressureConfig{
			QueueSize:  10000,
			DropPolicy: DropOldest,
		},
		Health: HealthConfig{
			Enabled:       true,
			CheckInterval: 30 * time.Second,
		},
		Failure: FailureConfig{
			Policy: FailPolicyIsolate, // Graceful degradation
			Retry: RetryConfig{
				MaxAttempts:  3,
				InitialDelay: 5 * time.Second,
				MaxDelay:     60 * time.Second,
				Multiplier:   2.0,
			},
		},
		Metrics: MetricsConfig{
			Enabled: true,
			AllowedLabels: []string{
				"namespace",
				"observer_type",
				// NOT pod_name, container_id (too high cardinality)
			},
		},
	}
}
