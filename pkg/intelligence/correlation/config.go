package correlation

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// CorrelationConfig holds all configuration for correlation engine and correlators
type CorrelationConfig struct {
	Engine     EngineConfiguration     `json:"engine"`
	Temporal   TemporalConfiguration   `json:"temporal"`
	Sequence   SequenceConfiguration   `json:"sequence"`
	Memory     MemoryStorageConfig     `json:"memory"`
	Processing ProcessingConfiguration `json:"processing"`
}

// EngineConfiguration holds engine-specific configuration
type EngineConfiguration struct {
	// Buffer sizes
	EventBufferSize   int `json:"event_buffer_size"`
	ResultBufferSize  int `json:"result_buffer_size"`
	ChannelBufferSize int `json:"channel_buffer_size"`

	// Worker configuration
	WorkerCount       int           `json:"worker_count"`
	ProcessingTimeout time.Duration `json:"processing_timeout"`
	CorrelationWindow time.Duration `json:"correlation_window"`
	PatternTimeout    time.Duration `json:"pattern_timeout"`

	// Storage worker pool configuration
	StorageWorkerCount int `json:"storage_worker_count"`
	StorageQueueSize   int `json:"storage_queue_size"`

	// Feature flags
	EnableK8s         bool `json:"enable_k8s"`
	EnableTemporal    bool `json:"enable_temporal"`
	EnableSequence    bool `json:"enable_sequence"`
	EnablePerformance bool `json:"enable_performance"`
	EnableServiceMap  bool `json:"enable_service_map"`

	// Storage configuration
	StorageCleanupInterval time.Duration `json:"storage_cleanup_interval"`
	StorageRetention       time.Duration `json:"storage_retention"`
}

// TemporalConfiguration holds temporal correlator configuration
type TemporalConfiguration struct {
	// Windows and timeouts
	TimeWindow        time.Duration `json:"time_window"`
	ConfigWindow      time.Duration `json:"config_window"`
	RestartWindow     time.Duration `json:"restart_window"`
	PodStartupWindow  time.Duration `json:"pod_startup_window"`
	ServiceMetricsWin time.Duration `json:"service_metrics_window"`

	// Pattern timeouts
	DefaultPatternTimeout  time.Duration `json:"default_pattern_timeout"`
	LongPatternTimeout     time.Duration `json:"long_pattern_timeout"`
	ExtendedPatternTimeout time.Duration `json:"extended_pattern_timeout"`

	// Limits
	MaxTemporalItems   int           `json:"max_temporal_items"`
	MaxPatternsTracked int           `json:"max_patterns_tracked"`
	MaxEventAge        time.Duration `json:"max_event_age"`
}

// SequenceConfiguration holds sequence correlator configuration
type SequenceConfiguration struct {
	// Sequence windows
	MaxSequenceAge    time.Duration `json:"max_sequence_age"`
	MaxSequenceGap    time.Duration `json:"max_sequence_gap"`
	MinSequenceLength int           `json:"min_sequence_length"`
	SequenceWindow    time.Duration `json:"sequence_window"`

	// Limits
	MaxActiveSequences int `json:"max_active_sequences"`
}

// ProcessingConfiguration holds processing-related configuration
type ProcessingConfiguration struct {
	// Retry configuration
	DefaultRetryDelay time.Duration `json:"default_retry_delay"`
	MaxRetryAttempts  int           `json:"max_retry_attempts"`
	ProcessingDelay   time.Duration `json:"processing_delay"`

	// Performance thresholds
	SlowProcessingThreshold time.Duration `json:"slow_processing_threshold"`
	HighLatencyThresholdMs  int64         `json:"high_latency_threshold_ms"`
	MaxEventsPerCorrelation int           `json:"max_events_per_correlation"`

	// Query limits
	DefaultQueryLimit   int `json:"default_query_limit"`
	MaxQueryLimit       int `json:"max_query_limit"`
	ServiceQueryLimit   int `json:"service_query_limit"`
	OwnershipQueryLimit int `json:"ownership_query_limit"`
	PodQueryLimit       int `json:"pod_query_limit"`
}

// MemoryStorageConfig holds memory storage configuration
type MemoryStorageConfig struct {
	// Storage bounds
	MaxSize                 int           `json:"max_size"`
	MaxAge                  time.Duration `json:"max_age"`
	MaxCorrelationsPerTrace int           `json:"max_correlations_per_trace"`
	MaxTimeEntries          int           `json:"max_time_entries"`

	// Eviction policies
	EvictionPolicy string `json:"eviction_policy"` // "lru", "lfu", "ttl"
	MemoryLimit    int64  `json:"memory_limit"`    // bytes
}

// DefaultCorrelationConfig returns the default configuration with all timeouts and limits
func DefaultCorrelationConfig() *CorrelationConfig {
	return &CorrelationConfig{
		Engine:     buildEngineConfig(),
		Temporal:   buildTemporalConfig(),
		Sequence:   buildSequenceConfig(),
		Memory:     buildMemoryConfig(),
		Processing: buildProcessingConfig(),
	}
}

// buildEngineConfig creates default engine configuration
func buildEngineConfig() EngineConfiguration {
	return EngineConfiguration{
		EventBufferSize:        getEnvInt("CORRELATION_EVENT_BUFFER_SIZE", 1000),
		ResultBufferSize:       getEnvInt("CORRELATION_RESULT_BUFFER_SIZE", 1000),
		WorkerCount:            getEnvInt("CORRELATION_WORKER_COUNT", 4),
		StorageWorkerCount:     getEnvInt("CORRELATION_STORAGE_WORKER_COUNT", 10),
		StorageQueueSize:       getEnvInt("CORRELATION_STORAGE_QUEUE_SIZE", 100),
		ProcessingTimeout:      getEnvDuration("CORRELATION_PROCESSING_TIMEOUT", 30*time.Second),
		EnableK8s:              getEnvBool("CORRELATION_ENABLE_K8S", true),
		EnableTemporal:         getEnvBool("CORRELATION_ENABLE_TEMPORAL", true),
		EnableSequence:         getEnvBool("CORRELATION_ENABLE_SEQUENCE", true),
		EnablePerformance:      getEnvBool("CORRELATION_ENABLE_PERFORMANCE", true),
		EnableServiceMap:       getEnvBool("CORRELATION_ENABLE_SERVICEMAP", true),
		StorageCleanupInterval: getEnvDuration("CORRELATION_STORAGE_CLEANUP_INTERVAL", 5*time.Minute),
		StorageRetention:       getEnvDuration("CORRELATION_STORAGE_RETENTION", 24*time.Hour),
	}
}

// buildTemporalConfig creates default temporal configuration
func buildTemporalConfig() TemporalConfiguration {
	return TemporalConfiguration{
		TimeWindow:             getEnvDuration("TEMPORAL_TIME_WINDOW", 5*time.Minute),
		ConfigWindow:           getEnvDuration("TEMPORAL_CONFIG_WINDOW", 30*time.Minute),
		RestartWindow:          getEnvDuration("TEMPORAL_RESTART_WINDOW", 10*time.Minute),
		PodStartupWindow:       getEnvDuration("TEMPORAL_POD_STARTUP_WINDOW", 5*time.Minute),
		ServiceMetricsWin:      getEnvDuration("TEMPORAL_SERVICE_METRICS_WINDOW", 5*time.Minute),
		DefaultPatternTimeout:  getEnvDuration("TEMPORAL_DEFAULT_PATTERN_TIMEOUT", 2*time.Minute),
		LongPatternTimeout:     getEnvDuration("TEMPORAL_LONG_PATTERN_TIMEOUT", 5*time.Minute),
		ExtendedPatternTimeout: getEnvDuration("TEMPORAL_EXTENDED_PATTERN_TIMEOUT", 10*time.Minute),
		MaxTemporalItems:       getEnvInt("TEMPORAL_MAX_ITEMS", 10000),
		MaxPatternsTracked:     getEnvInt("TEMPORAL_MAX_PATTERNS", 1000),
		MaxEventAge:            getEnvDuration("TEMPORAL_MAX_EVENT_AGE", 24*time.Hour),
	}
}

// buildSequenceConfig creates default sequence configuration
func buildSequenceConfig() SequenceConfiguration {
	return SequenceConfiguration{
		MaxSequenceAge:     getEnvDuration("SEQUENCE_MAX_AGE", 15*time.Minute),
		MaxSequenceGap:     getEnvDuration("SEQUENCE_MAX_GAP", 3*time.Minute),
		MinSequenceLength:  getEnvInt("SEQUENCE_MIN_LENGTH", 3),
		SequenceWindow:     getEnvDuration("SEQUENCE_WINDOW", 5*time.Second),
		MaxActiveSequences: getEnvInt("SEQUENCE_MAX_ACTIVE", 1000),
	}
}

// buildMemoryConfig creates default memory configuration
func buildMemoryConfig() MemoryStorageConfig {
	return MemoryStorageConfig{
		MaxSize: getEnvInt("MEMORY_STORAGE_MAX_SIZE", 10000),
		MaxAge:  getEnvDuration("MEMORY_STORAGE_MAX_AGE", 24*time.Hour),
	}
}

// buildProcessingConfig creates default processing configuration
func buildProcessingConfig() ProcessingConfiguration {
	return ProcessingConfiguration{
		DefaultRetryDelay:       getEnvDuration("PROCESSING_RETRY_DELAY", 1*time.Second),
		MaxRetryAttempts:        getEnvInt("PROCESSING_MAX_RETRIES", 3),
		ProcessingDelay:         getEnvDuration("PROCESSING_DELAY", 100*time.Millisecond),
		SlowProcessingThreshold: getEnvDuration("PROCESSING_SLOW_THRESHOLD", 100*time.Millisecond),
		HighLatencyThresholdMs:  getEnvInt64("PROCESSING_HIGH_LATENCY_MS", 1000),
		MaxEventsPerCorrelation: getEnvInt("PROCESSING_MAX_EVENTS_PER_CORRELATION", 50),
		DefaultQueryLimit:       getEnvInt("QUERY_DEFAULT_LIMIT", 100),
		MaxQueryLimit:           getEnvInt("QUERY_MAX_LIMIT", 1000),
		ServiceQueryLimit:       getEnvInt("QUERY_SERVICE_LIMIT", 100),
		OwnershipQueryLimit:     getEnvInt("QUERY_OWNERSHIP_LIMIT", 100),
		PodQueryLimit:           getEnvInt("QUERY_POD_LIMIT", 100),
	}
}

// Validate validates the configuration
func (c *CorrelationConfig) Validate() error {
	if err := c.Engine.Validate(); err != nil {
		return fmt.Errorf("engine config validation failed: %w", err)
	}
	if err := c.Temporal.Validate(); err != nil {
		return fmt.Errorf("temporal config validation failed: %w", err)
	}
	if err := c.Sequence.Validate(); err != nil {
		return fmt.Errorf("sequence config validation failed: %w", err)
	}
	if err := c.Memory.Validate(); err != nil {
		return fmt.Errorf("memory config validation failed: %w", err)
	}
	if err := c.Processing.Validate(); err != nil {
		return fmt.Errorf("processing config validation failed: %w", err)
	}
	return nil
}

// Validate validates engine configuration
func (e *EngineConfiguration) Validate() error {
	if e.EventBufferSize <= 0 {
		return fmt.Errorf("event buffer size must be positive")
	}
	if e.ResultBufferSize <= 0 {
		return fmt.Errorf("result buffer size must be positive")
	}
	if e.WorkerCount <= 0 {
		return fmt.Errorf("worker count must be positive")
	}
	if e.StorageWorkerCount < 0 {
		return fmt.Errorf("storage worker count cannot be negative")
	}
	if e.StorageWorkerCount > 100 {
		return fmt.Errorf("storage worker count must be <= 100 to prevent resource exhaustion")
	}
	if e.StorageQueueSize < 0 {
		return fmt.Errorf("storage queue size cannot be negative")
	}
	if e.StorageQueueSize > 10000 {
		return fmt.Errorf("storage queue size must be <= 10000 to prevent memory exhaustion")
	}
	if e.ProcessingTimeout <= 0 {
		return fmt.Errorf("processing timeout must be positive")
	}
	if e.StorageCleanupInterval <= 0 {
		return fmt.Errorf("storage cleanup interval must be positive")
	}
	if e.StorageRetention <= 0 {
		return fmt.Errorf("storage retention must be positive")
	}
	return nil
}

// Validate validates temporal configuration
func (t *TemporalConfiguration) Validate() error {
	if t.TimeWindow <= 0 {
		return fmt.Errorf("time window must be positive")
	}
	if t.MaxTemporalItems <= 0 {
		return fmt.Errorf("max temporal items must be positive")
	}
	if t.MaxPatternsTracked <= 0 {
		return fmt.Errorf("max patterns tracked must be positive")
	}
	if t.MaxEventAge <= 0 {
		return fmt.Errorf("max event age must be positive")
	}
	return nil
}

// Validate validates sequence configuration
func (s *SequenceConfiguration) Validate() error {
	if s.MaxSequenceAge <= 0 {
		return fmt.Errorf("max sequence age must be positive")
	}
	if s.MaxSequenceGap <= 0 {
		return fmt.Errorf("max sequence gap must be positive")
	}
	if s.MinSequenceLength <= 0 {
		return fmt.Errorf("min sequence length must be positive")
	}
	if s.MaxActiveSequences <= 0 {
		return fmt.Errorf("max active sequences must be positive")
	}
	return nil
}

// Validate validates processing configuration
func (p *ProcessingConfiguration) Validate() error {
	if p.MaxRetryAttempts < 0 {
		return fmt.Errorf("max retry attempts cannot be negative")
	}
	if p.DefaultQueryLimit <= 0 {
		return fmt.Errorf("default query limit must be positive")
	}
	if p.MaxQueryLimit <= 0 {
		return fmt.Errorf("max query limit must be positive")
	}
	if p.MaxQueryLimit < p.DefaultQueryLimit {
		return fmt.Errorf("max query limit must be >= default query limit")
	}
	return nil
}

// Validate validates memory storage configuration
func (m *MemoryStorageConfig) Validate() error {
	if m.MaxSize <= 0 {
		return fmt.Errorf("max size must be positive")
	}
	if m.MaxAge <= 0 {
		return fmt.Errorf("max age must be positive")
	}
	return nil
}

// Helper functions for environment variable parsing

func getEnvInt(key string, defaultValue int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvInt64(key string, defaultValue int64) int64 {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if val := os.Getenv(key); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultValue
}

func getEnvString(key, defaultValue string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultValue
}

// EngineConfig holds configuration for the correlation engine (compatibility type)
type EngineConfig struct {
	// Processing timeouts
	ProcessingTimeout time.Duration `json:"processing_timeout"`
	CorrelationWindow time.Duration `json:"correlation_window"`
	PatternTimeout    time.Duration `json:"pattern_timeout"`

	// Buffer sizes
	EventBufferSize   int `json:"event_buffer_size"`
	ResultBufferSize  int `json:"result_buffer_size"`
	ChannelBufferSize int `json:"channel_buffer_size"`

	// Worker configuration
	WorkerCount        int `json:"worker_count"`
	StorageWorkerCount int `json:"storage_worker_count"`
	StorageQueueSize   int `json:"storage_queue_size"`

	// Storage configuration
	StorageCleanupInterval time.Duration `json:"storage_cleanup_interval"`
	StorageRetention       time.Duration `json:"storage_retention"`

	// Query limits
	DefaultQueryLimit   int `json:"default_query_limit"`
	MaxQueryLimit       int `json:"max_query_limit"`
	ServiceQueryLimit   int `json:"service_query_limit"`
	OwnershipQueryLimit int `json:"ownership_query_limit"`
	PodQueryLimit       int `json:"pod_query_limit"`

	// Correlation parameters
	MinConfidence      float64 `json:"min_confidence"`
	MaxActiveSequences int     `json:"max_active_sequences"`
	MaxPatternsTracked int     `json:"max_patterns_tracked"`
	MaxEventsPerCorr   int     `json:"max_events_per_correlation"`

	// Kubernetes settings
	KubernetesNamespace string   `json:"kubernetes_namespace"`
	EnabledCorrelators  []string `json:"enabled_correlators"`
}

// DefaultEngineConfig returns production-ready defaults
func DefaultEngineConfig() *EngineConfig {
	config := &EngineConfig{}
	setEngineTimeouts(config)
	setEngineBuffers(config)
	setEngineQueryLimits(config)
	setEngineCorrelationSettings(config)
	setEngineDefaults(config)
	return config
}

// setEngineTimeouts configures timeout settings
func setEngineTimeouts(config *EngineConfig) {
	config.ProcessingTimeout = DefaultProcessingTimeout
	config.CorrelationWindow = DefaultPatternTimeout
	config.PatternTimeout = DefaultPatternTimeout
	config.StorageCleanupInterval = ServiceMetricsWindow
	config.StorageRetention = MaxEventAge
}

// setEngineBuffers configures buffer sizes
func setEngineBuffers(config *EngineConfig) {
	config.EventBufferSize = DefaultEventBufferSize
	config.ResultBufferSize = DefaultResultBufferSize
	config.ChannelBufferSize = DefaultChannelBuffer
	config.WorkerCount = 4
	config.StorageWorkerCount = 10
	config.StorageQueueSize = 100
}

// setEngineQueryLimits configures query limits
func setEngineQueryLimits(config *EngineConfig) {
	config.DefaultQueryLimit = DefaultQueryLimit
	config.MaxQueryLimit = MaxQueryLimit
	config.ServiceQueryLimit = ServiceQueryLimit
	config.OwnershipQueryLimit = OwnershipQueryLimit
	config.PodQueryLimit = PodQueryLimit
}

// setEngineCorrelationSettings configures correlation parameters
func setEngineCorrelationSettings(config *EngineConfig) {
	config.MinConfidence = MinConfidenceThreshold
	config.MaxActiveSequences = MaxActiveSequences
	config.MaxPatternsTracked = MaxPatternsTracked
	config.MaxEventsPerCorr = MaxEventsPerCorrelation
}

// setEngineDefaults configures default settings
func setEngineDefaults(config *EngineConfig) {
	config.KubernetesNamespace = DefaultNamespace
	config.EnabledCorrelators = []string{
		"dependency", "temporal", "ownership", "config-impact",
		"k8s", "sequence", "servicemap",
	}
}

// TestEngineConfig returns configuration optimized for testing
func TestEngineConfig() *EngineConfig {
	return &EngineConfig{
		ProcessingTimeout:      TestProcessingTimeout,
		CorrelationWindow:      ShortTestTimeout,
		PatternTimeout:         ShortTestTimeout,
		EventBufferSize:        TestEventBufferSize,
		ResultBufferSize:       TestResultBufferSize,
		ChannelBufferSize:      TestChannelBuffer,
		WorkerCount:            2,
		StorageWorkerCount:     2,
		StorageQueueSize:       20,
		StorageCleanupInterval: TestCleanupInterval,
		StorageRetention:       TestRetention,
		DefaultQueryLimit:      DefaultQueryLimit,
		MaxQueryLimit:          MaxQueryLimit,
		ServiceQueryLimit:      ServiceQueryLimit,
		OwnershipQueryLimit:    OwnershipQueryLimit,
		PodQueryLimit:          PodQueryLimit,
		MinConfidence:          MinConfidenceThreshold,
		MaxActiveSequences:     TestMaxActiveSequences,
		MaxPatternsTracked:     MaxPatternsTracked,
		MaxEventsPerCorr:       MaxEventsPerCorrelation,
		KubernetesNamespace:    DefaultNamespace,
		EnabledCorrelators:     []string{"dependency", "temporal", "ownership", "config-impact"},
	}
}

// Validate checks if engine configuration is valid according to CLAUDE.md standards
func (c *EngineConfig) Validate() error {
	if err := c.validateTimeouts(); err != nil {
		return fmt.Errorf("timeout validation failed: %w", err)
	}
	if err := c.validateBuffers(); err != nil {
		return fmt.Errorf("buffer validation failed: %w", err)
	}
	if err := c.validateQueryLimits(); err != nil {
		return fmt.Errorf("query limit validation failed: %w", err)
	}
	if err := c.validateCorrelationSettings(); err != nil {
		return fmt.Errorf("correlation settings validation failed: %w", err)
	}
	if err := c.validateCorrelators(); err != nil {
		return fmt.Errorf("correlator validation failed: %w", err)
	}
	return nil
}

// validateTimeouts validates timeout configuration
func (c *EngineConfig) validateTimeouts() error {
	if c.ProcessingTimeout <= 0 {
		return fmt.Errorf("processing timeout must be positive, got: %v", c.ProcessingTimeout)
	}
	if c.CorrelationWindow <= 0 {
		return fmt.Errorf("correlation window must be positive, got: %v", c.CorrelationWindow)
	}
	if c.PatternTimeout <= 0 {
		return fmt.Errorf("pattern timeout must be positive, got: %v", c.PatternTimeout)
	}
	return nil
}

// validateBuffers validates buffer size configuration
func (c *EngineConfig) validateBuffers() error {
	if c.EventBufferSize <= 0 {
		return fmt.Errorf("event buffer size must be positive, got: %d", c.EventBufferSize)
	}
	if c.ResultBufferSize <= 0 {
		return fmt.Errorf("result buffer size must be positive, got: %d", c.ResultBufferSize)
	}
	if c.StorageWorkerCount < 0 {
		return fmt.Errorf("storage worker count cannot be negative, got: %d", c.StorageWorkerCount)
	}
	if c.StorageWorkerCount > 100 {
		return fmt.Errorf("storage worker count must be <= 100, got: %d", c.StorageWorkerCount)
	}
	if c.StorageQueueSize < 0 {
		return fmt.Errorf("storage queue size cannot be negative, got: %d", c.StorageQueueSize)
	}
	if c.StorageQueueSize > 10000 {
		return fmt.Errorf("storage queue size must be <= 10000, got: %d", c.StorageQueueSize)
	}
	return nil
}

// validateQueryLimits validates query limit configuration
func (c *EngineConfig) validateQueryLimits() error {
	if c.DefaultQueryLimit <= 0 {
		return fmt.Errorf("default query limit must be positive, got: %d", c.DefaultQueryLimit)
	}
	if c.MaxQueryLimit < c.DefaultQueryLimit {
		return fmt.Errorf("max query limit (%d) must be >= default query limit (%d)", c.MaxQueryLimit, c.DefaultQueryLimit)
	}
	return nil
}

// validateCorrelationSettings validates correlation parameters
func (c *EngineConfig) validateCorrelationSettings() error {
	if c.MinConfidence < 0 || c.MinConfidence > MaxConfidenceValue {
		return fmt.Errorf("min confidence must be between 0 and %.1f, got: %.2f", MaxConfidenceValue, c.MinConfidence)
	}
	if c.MaxActiveSequences <= 0 {
		return fmt.Errorf("max active sequences must be positive, got: %d", c.MaxActiveSequences)
	}
	if c.MaxPatternsTracked <= 0 {
		return fmt.Errorf("max patterns tracked must be positive, got: %d", c.MaxPatternsTracked)
	}
	if c.MaxEventsPerCorr <= 0 {
		return fmt.Errorf("max events per correlation must be positive, got: %d", c.MaxEventsPerCorr)
	}
	return nil
}

// validateCorrelators validates correlator configuration
func (c *EngineConfig) validateCorrelators() error {
	if len(c.EnabledCorrelators) == 0 {
		return fmt.Errorf("at least one correlator must be enabled")
	}
	return nil
}

// SequenceConfig holds configuration for sequence correlators
type SequenceConfig struct {
	MaxSequenceAge     time.Duration `json:"max_sequence_age"`
	MaxSequenceGap     time.Duration `json:"max_sequence_gap"`
	MinSequenceLength  int           `json:"min_sequence_length"`
	MaxActiveSequences int           `json:"max_active_sequences"`
}

// DefaultSequenceConfig returns production sequence configuration
func DefaultSequenceConfig() *SequenceConfig {
	return &SequenceConfig{
		MaxSequenceAge:     DefaultMaxSequenceAge,
		MaxSequenceGap:     DefaultMaxSequenceGap,
		MinSequenceLength:  DefaultMinSequenceLength,
		MaxActiveSequences: MaxActiveSequences,
	}
}

// TestSequenceConfig returns test sequence configuration
func TestSequenceConfig() *SequenceConfig {
	return &SequenceConfig{
		MaxSequenceAge:     TestMaxSequenceAge,
		MaxSequenceGap:     TestMaxSequenceGap,
		MinSequenceLength:  TestMinSequenceLength,
		MaxActiveSequences: TestMaxActiveSequences,
	}
}

// Validate checks if sequence configuration is valid
func (s *SequenceConfig) Validate() error {
	if s.MaxSequenceAge <= 0 {
		return fmt.Errorf("max sequence age must be positive, got: %v", s.MaxSequenceAge)
	}

	if s.MaxSequenceGap <= 0 {
		return fmt.Errorf("max sequence gap must be positive, got: %v", s.MaxSequenceGap)
	}

	if s.MinSequenceLength <= 0 {
		return fmt.Errorf("min sequence length must be positive, got: %d", s.MinSequenceLength)
	}

	if s.MaxActiveSequences <= 0 {
		return fmt.Errorf("max active sequences must be positive, got: %d", s.MaxActiveSequences)
	}

	return nil
}

// TemporalConfig holds configuration for temporal correlators
type TemporalConfig struct {
	MaxPatternsTracked int           `json:"max_patterns_tracked"`
	PatternTimeout     time.Duration `json:"pattern_timeout"`
	MaxTemporalItems   int           `json:"max_temporal_items"`
	WindowSize         time.Duration `json:"window_size"`
	MinOccurrences     int           `json:"min_occurrences"`
}

// DefaultTemporalConfig returns production temporal configuration
func DefaultTemporalConfig() *TemporalConfig {
	return &TemporalConfig{
		MaxPatternsTracked: MaxPatternsTracked,
		PatternTimeout:     DefaultPatternTimeout,
		MaxTemporalItems:   MaxTemporalItems,
		WindowSize:         5 * time.Minute,
		MinOccurrences:     2,
	}
}

// TestTemporalConfig returns test temporal configuration
func TestTemporalConfig() *TemporalConfig {
	return &TemporalConfig{
		MaxPatternsTracked: MaxPatternsTracked,
		PatternTimeout:     ShortTestTimeout,
		MaxTemporalItems:   TestTemporalItems,
		WindowSize:         2 * time.Minute,
		MinOccurrences:     2,
	}
}

// Validate checks if temporal configuration is valid
func (t *TemporalConfig) Validate() error {
	if t.MaxPatternsTracked <= 0 {
		return fmt.Errorf("max patterns tracked must be positive, got: %d", t.MaxPatternsTracked)
	}

	if t.PatternTimeout <= 0 {
		return fmt.Errorf("pattern timeout must be positive, got: %v", t.PatternTimeout)
	}

	if t.MaxTemporalItems <= 0 {
		return fmt.Errorf("max temporal items must be positive, got: %d", t.MaxTemporalItems)
	}

	return nil
}
