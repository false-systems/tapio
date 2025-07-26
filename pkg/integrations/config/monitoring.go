package config

import "time"

// MonitoringConfig provides monitoring and observability configuration
type MonitoringConfig struct {
	BaseConfig `yaml:",inline" json:",inline"`

	// Metrics collection
	Metrics MetricsConfig `yaml:"metrics" json:"metrics"`

	// Performance monitoring
	Performance PerformanceConfig `yaml:"performance" json:"performance"`

	// Alerting
	Alerting AlertingConfig `yaml:"alerting" json:"alerting"`

	// Profiling
	Profiling ProfilingConfig `yaml:"profiling" json:"profiling"`
}

// MetricsConfig defines metrics collection settings
type MetricsConfig struct {
	Enabled       bool              `yaml:"enabled" json:"enabled"`
	Provider      string            `yaml:"provider" json:"provider"` // prometheus, otel, statsd
	Endpoint      string            `yaml:"endpoint" json:"endpoint"`
	Interval      time.Duration     `yaml:"interval" json:"interval"`
	Histograms    bool              `yaml:"histograms" json:"histograms"`
	Buckets       []float64         `yaml:"buckets" json:"buckets"`
	Quantiles     []float64         `yaml:"quantiles" json:"quantiles"`
	Labels        map[string]string `yaml:"labels" json:"labels"`
	Cardinality   CardinalityConfig `yaml:"cardinality" json:"cardinality"`
}

// CardinalityConfig defines cardinality limits
type CardinalityConfig struct {
	Enabled       bool `yaml:"enabled" json:"enabled"`
	MaxSeries     int  `yaml:"max_series" json:"max_series"`
	MaxLabels     int  `yaml:"max_labels" json:"max_labels"`
	MaxLabelValue int  `yaml:"max_label_value" json:"max_label_value"`
}

// PerformanceConfig defines performance monitoring settings
type PerformanceConfig struct {
	Enabled           bool                   `yaml:"enabled" json:"enabled"`
	CPUProfiling      bool                   `yaml:"cpu_profiling" json:"cpu_profiling"`
	MemoryProfiling   bool                   `yaml:"memory_profiling" json:"memory_profiling"`
	GoroutineTracking bool                   `yaml:"goroutine_tracking" json:"goroutine_tracking"`
	SLO               SLOConfig              `yaml:"slo" json:"slo"`
	Thresholds        PerformanceThresholds  `yaml:"thresholds" json:"thresholds"`
}

// SLOConfig defines Service Level Objectives
type SLOConfig struct {
	Enabled           bool              `yaml:"enabled" json:"enabled"`
	AvailabilityTarget float64          `yaml:"availability_target" json:"availability_target"`
	LatencyTargets    map[string]time.Duration `yaml:"latency_targets" json:"latency_targets"`
	ErrorBudget       float64           `yaml:"error_budget" json:"error_budget"`
	Window            time.Duration     `yaml:"window" json:"window"`
}

// PerformanceThresholds defines performance thresholds
type PerformanceThresholds struct {
	CPUWarning      float64       `yaml:"cpu_warning" json:"cpu_warning"`
	CPUCritical     float64       `yaml:"cpu_critical" json:"cpu_critical"`
	MemoryWarning   float64       `yaml:"memory_warning" json:"memory_warning"`
	MemoryCritical  float64       `yaml:"memory_critical" json:"memory_critical"`
	LatencyWarning  time.Duration `yaml:"latency_warning" json:"latency_warning"`
	LatencyCritical time.Duration `yaml:"latency_critical" json:"latency_critical"`
	ErrorRateWarning float64      `yaml:"error_rate_warning" json:"error_rate_warning"`
	ErrorRateCritical float64     `yaml:"error_rate_critical" json:"error_rate_critical"`
}

// AlertingConfig defines alerting settings
type AlertingConfig struct {
	Enabled    bool           `yaml:"enabled" json:"enabled"`
	Providers  []AlertProvider `yaml:"providers" json:"providers"`
	Rules      []AlertRule     `yaml:"rules" json:"rules"`
	Silences   []AlertSilence  `yaml:"silences" json:"silences"`
	Grouping   AlertGrouping   `yaml:"grouping" json:"grouping"`
	Throttling AlertThrottling `yaml:"throttling" json:"throttling"`
}

// AlertProvider defines an alert provider
type AlertProvider struct {
	Name     string            `yaml:"name" json:"name"`
	Type     string            `yaml:"type" json:"type"` // webhook, email, slack, pagerduty
	Endpoint string            `yaml:"endpoint" json:"endpoint"`
	Headers  map[string]string `yaml:"headers" json:"headers"`
	Timeout  time.Duration     `yaml:"timeout" json:"timeout"`
}

// AlertRule defines an alerting rule
type AlertRule struct {
	Name        string            `yaml:"name" json:"name"`
	Expression  string            `yaml:"expression" json:"expression"`
	Duration    time.Duration     `yaml:"duration" json:"duration"`
	Severity    string            `yaml:"severity" json:"severity"`
	Labels      map[string]string `yaml:"labels" json:"labels"`
	Annotations map[string]string `yaml:"annotations" json:"annotations"`
}

// AlertSilence defines alert silence period
type AlertSilence struct {
	Matchers  map[string]string `yaml:"matchers" json:"matchers"`
	StartTime time.Time         `yaml:"start_time" json:"start_time"`
	EndTime   time.Time         `yaml:"end_time" json:"end_time"`
	Comment   string            `yaml:"comment" json:"comment"`
}

// AlertGrouping defines alert grouping
type AlertGrouping struct {
	Enabled      bool          `yaml:"enabled" json:"enabled"`
	GroupBy      []string      `yaml:"group_by" json:"group_by"`
	GroupWait    time.Duration `yaml:"group_wait" json:"group_wait"`
	GroupInterval time.Duration `yaml:"group_interval" json:"group_interval"`
}

// AlertThrottling defines alert throttling
type AlertThrottling struct {
	Enabled       bool          `yaml:"enabled" json:"enabled"`
	MaxPerHour    int           `yaml:"max_per_hour" json:"max_per_hour"`
	CooldownPeriod time.Duration `yaml:"cooldown_period" json:"cooldown_period"`
}

// ProfilingConfig defines profiling settings
type ProfilingConfig struct {
	Enabled      bool                `yaml:"enabled" json:"enabled"`
	CPU          bool                `yaml:"cpu" json:"cpu"`
	Memory       bool                `yaml:"memory" json:"memory"`
	Goroutine    bool                `yaml:"goroutine" json:"goroutine"`
	Mutex        bool                `yaml:"mutex" json:"mutex"`
	Block        bool                `yaml:"block" json:"block"`
	Endpoint     string              `yaml:"endpoint" json:"endpoint"`
	SampleRate   int                 `yaml:"sample_rate" json:"sample_rate"`
	UploadPeriod time.Duration       `yaml:"upload_period" json:"upload_period"`
	Labels       map[string]string   `yaml:"labels" json:"labels"`
}

// DefaultMonitoringConfig returns default monitoring configuration
func DefaultMonitoringConfig() MonitoringConfig {
	return MonitoringConfig{
		BaseConfig: DefaultBaseConfig(),
		Metrics: MetricsConfig{
			Enabled:  true,
			Provider: "prometheus",
			Interval: 60 * time.Second,
			Histograms: true,
			Buckets:  []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			Quantiles: []float64{0.5, 0.9, 0.95, 0.99},
		},
		Performance: PerformanceConfig{
			Enabled: true,
			SLO: SLOConfig{
				Enabled:            true,
				AvailabilityTarget: 99.9,
				ErrorBudget:        0.1,
				Window:             24 * time.Hour,
			},
			Thresholds: PerformanceThresholds{
				CPUWarning:        70.0,
				CPUCritical:       90.0,
				MemoryWarning:     75.0,
				MemoryCritical:    90.0,
				LatencyWarning:    1 * time.Second,
				LatencyCritical:   5 * time.Second,
				ErrorRateWarning:  0.01,
				ErrorRateCritical: 0.05,
			},
		},
		Alerting: AlertingConfig{
			Enabled: true,
			Grouping: AlertGrouping{
				Enabled:       true,
				GroupWait:     10 * time.Second,
				GroupInterval: 5 * time.Minute,
			},
			Throttling: AlertThrottling{
				Enabled:        true,
				MaxPerHour:     10,
				CooldownPeriod: 1 * time.Hour,
			},
		},
	}
}