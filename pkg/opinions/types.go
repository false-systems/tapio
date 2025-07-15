package opinions

import (
	"time"
)

// OpinionConfig represents the complete opinion configuration
type OpinionConfig struct {
	// Metadata about the config
	Metadata map[string]string `yaml:"metadata,omitempty"`

	// Core opinions
	ImportanceWeights  map[string]float32       `yaml:"importance_weights,omitempty"`
	CorrelationWindows map[string]time.Duration `yaml:"correlation_windows,omitempty"`
	AnomalyThresholds  map[string]float32       `yaml:"anomaly_thresholds,omitempty"`

	// Behavioral configuration
	BehavioralConfig BehavioralOpinions `yaml:"behavioral,omitempty"`

	// Prediction configuration
	PredictionConfig PredictionOpinions `yaml:"prediction,omitempty"`

	// Service-specific limits
	ServiceLimits map[string]ServiceLimit `yaml:"service_limits,omitempty"`

	// Service dependencies
	ServiceDependencies []ServiceDependency `yaml:"service_dependencies,omitempty"`

	// Time-based rules
	TimeBasedRules []TimeBasedRule `yaml:"time_based_rules,omitempty"`

	// Profile information
	Profile     string `yaml:"profile,omitempty"`
	BaseProfile string `yaml:"base_profile,omitempty"`
}

// BehavioralOpinions for behavior analysis
type BehavioralOpinions struct {
	LearningWindow       time.Duration `yaml:"learning_window,omitempty"`
	MinSamplesRequired   int           `yaml:"min_samples_required,omitempty"`
	DeviationSensitivity float32       `yaml:"deviation_sensitivity,omitempty"`
	TrendWindow          time.Duration `yaml:"trend_window,omitempty"`
}

// PredictionOpinions for predictive capabilities
type PredictionOpinions struct {
	EnableOOMPrediction     bool                     `yaml:"enable_oom_prediction"`
	EnableCascadePrediction bool                     `yaml:"enable_cascade_prediction"`
	EnableAnomalyPrediction bool                     `yaml:"enable_anomaly_prediction"`
	PredictionHorizon       time.Duration            `yaml:"prediction_horizon,omitempty"`
	MinConfidenceThreshold  float32                  `yaml:"min_confidence_threshold,omitempty"`
	PredictionWindows       map[string]time.Duration `yaml:"prediction_windows,omitempty"`
}

// ServiceLimit defines resource limits for a service
type ServiceLimit struct {
	MemoryLimit float32                `yaml:"memory_limit,omitempty"`
	CPULimit    float32                `yaml:"cpu_limit,omitempty"`
	CustomRules map[string]interface{} `yaml:"custom_rules,omitempty"`
}

// ServiceDependency defines a dependency between services
type ServiceDependency struct {
	Source        string        `yaml:"source"`
	Target        string        `yaml:"target"`
	ExpectedDelay time.Duration `yaml:"expected_delay"`
	Confidence    float32       `yaml:"confidence,omitempty"`
}

// TimeBasedRule defines time-based sensitivity rules
type TimeBasedRule struct {
	Period      string   `yaml:"period"`
	Sensitivity float32  `yaml:"sensitivity"`
	Description string   `yaml:"description,omitempty"`
	TimeRange   string   `yaml:"time_range,omitempty"` // e.g., "09:00-17:00"
	Weekdays    []string `yaml:"weekdays,omitempty"`   // e.g., ["Mon", "Tue", "Wed", "Thu", "Fri"]
}

// OpinionTemplate represents a pre-configured opinion set
type OpinionTemplate struct {
	Name        string        `yaml:"name"`
	Description string        `yaml:"description"`
	Tags        []string      `yaml:"tags,omitempty"`
	Config      OpinionConfig `yaml:"config"`
}

// ValidationResult contains validation results
type ValidationResult struct {
	Valid    bool
	Errors   []ValidationError
	Warnings []ValidationWarning
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
	Value   interface{}
}

// ValidationWarning represents a validation warning
type ValidationWarning struct {
	Field   string
	Message string
	Value   interface{}
}
