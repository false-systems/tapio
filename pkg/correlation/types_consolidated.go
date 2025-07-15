package correlation

import (
	"sync"
	"time"
)

// =============================================================================
// CONSOLIDATED TYPE DEFINITIONS
// This file contains the canonical definitions of all correlation types
// to eliminate redeclarations and provide a single source of truth
// =============================================================================

// Prediction represents a future event prediction (Enhanced OTEL version)
type Prediction struct {
	ID               string                 `json:"id"`
	Type             PredictionType         `json:"type"`
	Description      string                 `json:"description"`
	Value            float64                `json:"value"`
	Probability      float64                `json:"probability"`
	Confidence       float64                `json:"confidence"`
	TimeToEvent      time.Duration          `json:"time_to_event"`
	PredictionWindow time.Duration          `json:"prediction_window"`
	Entity           string                 `json:"entity"`
	Tags             map[string]string      `json:"tags"`
	CreatedAt        time.Time              `json:"created_at"`
	ExpiresAt        time.Time              `json:"expires_at"`
}

// PredictionType defines the type of prediction
type PredictionType string

const (
	PredictionTypeMetric    PredictionType = "metric"
	PredictionTypeAnomaly   PredictionType = "anomaly"
	PredictionTypeFailure   PredictionType = "failure"
	PredictionTypeResource  PredictionType = "resource"
)

// TrendAnalyzer analyzes trends in metrics (Enhanced OTEL version)
type TrendAnalyzer struct {
	timeSeries  map[string][]float64
	timestamps  map[string][]time.Time
	trendModels map[string]*TrendModel
	mutex       sync.RWMutex
}

// TrendModel represents a trend analysis model
type TrendModel struct {
	Slope        float64   `json:"slope"`
	Intercept    float64   `json:"intercept"`
	R2Score      float64   `json:"r2_score"`
	Direction    string    `json:"direction"` // "increasing", "decreasing", "stable"
	Velocity     float64   `json:"velocity"`
	Acceleration float64   `json:"acceleration"`
	LastUpdated  time.Time `json:"last_updated"`
}

// PerformanceOptimizer provides comprehensive performance optimization (Enhanced version)
type PerformanceOptimizer struct {
	// Core components
	engine    *PatternIntegratedEngine
	profiler  *PerformanceProfiler
	optimizer *AdaptiveOptimizer
	monitor   *PerformanceMonitor

	// Optimization state
	config         *OptimizationConfig
	optimizations  map[string]*OptimizationStrategy
	activeProfiles map[string]*PerformanceProfile

	// State management
	running          bool
	optimizationChan chan *OptimizationRequest
	mutex            sync.RWMutex
}

// OptimizationConfig configures performance optimization behavior
type OptimizationConfig struct {
	// Monitoring settings
	ProfilingEnabled      bool                   `json:"profiling_enabled"`
	MonitoringInterval    time.Duration          `json:"monitoring_interval"`
	PerformanceThresholds *PerformanceThresholds `json:"performance_thresholds"`

	// Optimization settings
	AutoOptimizationEnabled bool          `json:"auto_optimization_enabled"`
	OptimizationInterval    time.Duration `json:"optimization_interval"`
	MaxOptimizationAttempts int           `json:"max_optimization_attempts"`

	// Resource limits
	MaxCPUUsage    float64 `json:"max_cpu_usage"`
	MaxMemoryUsage int64   `json:"max_memory_usage"`
	MaxGoroutines  int     `json:"max_goroutines"`
}

// PerformanceThresholds defines performance thresholds
type PerformanceThresholds struct {
	MaxLatency        time.Duration `json:"max_latency"`
	MaxThroughput     float64       `json:"max_throughput"`
	MaxErrorRate      float64       `json:"max_error_rate"`
	MaxMemoryUsage    int64         `json:"max_memory_usage"`
	MaxCPUUsage       float64       `json:"max_cpu_usage"`
	MaxGoroutines     int           `json:"max_goroutines"`
	MaxQueueDepth     int           `json:"max_queue_depth"`
	MaxResponseTime   time.Duration `json:"max_response_time"`
	MaxProcessingTime time.Duration `json:"max_processing_time"`
}

// OptimizationStrategy defines an optimization strategy
type OptimizationStrategy struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Type        string                 `json:"type"`
	Priority    int                    `json:"priority"`
	Conditions  map[string]interface{} `json:"conditions"`
	Actions     []OptimizationAction   `json:"actions"`
	Metrics     *OptimizationMetrics   `json:"metrics"`
}

// OptimizationAction defines an optimization action
type OptimizationAction struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
	Timeout     time.Duration          `json:"timeout"`
}

// OptimizationMetrics tracks optimization performance
type OptimizationMetrics struct {
	AppliedCount    int64         `json:"applied_count"`
	SuccessCount    int64         `json:"success_count"`
	FailureCount    int64         `json:"failure_count"`
	AvgImprovement  float64       `json:"avg_improvement"`
	LastApplied     time.Time     `json:"last_applied"`
	LastImprovement float64       `json:"last_improvement"`
	TotalImprovement float64      `json:"total_improvement"`
}

// PerformanceProfile represents a performance profile
type PerformanceProfile struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Type             string                 `json:"type"`
	Metrics          map[string]float64     `json:"metrics"`
	Thresholds       *PerformanceThresholds `json:"thresholds"`
	OptimizationHints []string             `json:"optimization_hints"`
	CreatedAt        time.Time              `json:"created_at"`
	LastUpdated      time.Time              `json:"last_updated"`
}

// OptimizationRequest represents a request for optimization
type OptimizationRequest struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Priority    int                    `json:"priority"`
	Target      string                 `json:"target"`
	Metrics     map[string]float64     `json:"metrics"`
	Context     map[string]interface{} `json:"context"`
	RequestedAt time.Time              `json:"requested_at"`
}

// =============================================================================
// CONSTRUCTOR FUNCTIONS (Consolidated)
// =============================================================================

// NewTrendAnalyzer creates a new trend analyzer
func NewTrendAnalyzer(config interface{}) *TrendAnalyzer {
	return &TrendAnalyzer{
		timeSeries:  make(map[string][]float64),
		timestamps:  make(map[string][]time.Time),
		trendModels: make(map[string]*TrendModel),
	}
}

// NewPerformanceOptimizer creates a new performance optimizer
func NewPerformanceOptimizer(config *OptimizationConfig) *PerformanceOptimizer {
	if config == nil {
		config = &OptimizationConfig{
			ProfilingEnabled:        true,
			MonitoringInterval:      time.Minute,
			AutoOptimizationEnabled: true,
			OptimizationInterval:    time.Minute * 5,
			MaxOptimizationAttempts: 3,
			MaxCPUUsage:             0.8,
			MaxMemoryUsage:          1024 * 1024 * 1024, // 1GB
			MaxGoroutines:           1000,
		}
	}

	return &PerformanceOptimizer{
		config:           config,
		optimizations:    make(map[string]*OptimizationStrategy),
		activeProfiles:   make(map[string]*PerformanceProfile),
		optimizationChan: make(chan *OptimizationRequest, 100),
	}
}

// =============================================================================
// BACKWARD COMPATIBILITY ALIASES
// These provide backward compatibility for existing code
// =============================================================================

// Legacy aliases for backward compatibility
type (
	// SimplePrediction is the legacy simple prediction type
	SimplePrediction = struct {
		Type        string
		Probability float64
		Confidence  float64
		TimeToEvent time.Duration
		Description string
	}

	// BasicTrendAnalyzer is the legacy basic trend analyzer
	BasicTrendAnalyzer = struct {
		config CorrelatorConfig
	}
)

// Helper functions for compatibility
func NewSimplePrediction(pType string, probability, confidence float64, timeToEvent time.Duration, description string) *Prediction {
	return &Prediction{
		Type:        PredictionType(pType),
		Probability: probability,
		Confidence:  confidence,
		TimeToEvent: timeToEvent,
		Description: description,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(timeToEvent),
	}
}

func NewBasicTrendAnalyzer(config CorrelatorConfig) *TrendAnalyzer {
	return NewTrendAnalyzer(config)
}