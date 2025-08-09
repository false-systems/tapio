package correlation

import (
	"fmt"
	"time"
)

// EngineConfig holds configuration for the correlation engine
type EngineConfig struct {
	// Processing timeouts
	ProcessingTimeout time.Duration `json:"processing_timeout"`
	CorrelationWindow time.Duration `json:"correlation_window"`
	PatternTimeout    time.Duration `json:"pattern_timeout"`

	// Buffer sizes
	EventBufferSize   int `json:"event_buffer_size"`
	ResultBufferSize  int `json:"result_buffer_size"`
	ChannelBufferSize int `json:"channel_buffer_size"`

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
	return &EngineConfig{
		ProcessingTimeout:   DefaultProcessingTimeout,
		CorrelationWindow:   DefaultPatternTimeout,
		PatternTimeout:      DefaultPatternTimeout,
		EventBufferSize:     DefaultEventBufferSize,
		ResultBufferSize:    DefaultResultBufferSize,
		ChannelBufferSize:   DefaultChannelBuffer,
		DefaultQueryLimit:   DefaultQueryLimit,
		MaxQueryLimit:       MaxQueryLimit,
		ServiceQueryLimit:   ServiceQueryLimit,
		OwnershipQueryLimit: OwnershipQueryLimit,
		PodQueryLimit:       PodQueryLimit,
		MinConfidence:       MinConfidenceThreshold,
		MaxActiveSequences:  MaxActiveSequences,
		MaxPatternsTracked:  MaxPatternsTracked,
		MaxEventsPerCorr:    MaxEventsPerCorrelation,
		KubernetesNamespace: DefaultNamespace,
		EnabledCorrelators:  []string{"dependency", "temporal", "ownership", "config-impact", "k8s", "sequence", "servicemap"},
	}
}

// TestEngineConfig returns configuration optimized for testing
func TestEngineConfig() *EngineConfig {
	return &EngineConfig{
		ProcessingTimeout:   TestProcessingTimeout,
		CorrelationWindow:   ShortTestTimeout,
		PatternTimeout:      ShortTestTimeout,
		EventBufferSize:     TestEventBufferSize,
		ResultBufferSize:    TestResultBufferSize,
		ChannelBufferSize:   TestChannelBuffer,
		DefaultQueryLimit:   DefaultQueryLimit,
		MaxQueryLimit:       MaxQueryLimit,
		ServiceQueryLimit:   ServiceQueryLimit,
		OwnershipQueryLimit: OwnershipQueryLimit,
		PodQueryLimit:       PodQueryLimit,
		MinConfidence:       MinConfidenceThreshold,
		MaxActiveSequences:  TestMaxActiveSequences,
		MaxPatternsTracked:  MaxPatternsTracked,
		MaxEventsPerCorr:    MaxEventsPerCorrelation,
		KubernetesNamespace: DefaultNamespace,
		EnabledCorrelators:  []string{"dependency", "temporal", "ownership", "config-impact"},
	}
}

// Validate checks if configuration is valid according to CLAUDE.md standards
func (c *EngineConfig) Validate() error {
	if c.ProcessingTimeout <= 0 {
		return fmt.Errorf("processing timeout must be positive, got: %v", c.ProcessingTimeout)
	}

	if c.CorrelationWindow <= 0 {
		return fmt.Errorf("correlation window must be positive, got: %v", c.CorrelationWindow)
	}

	if c.PatternTimeout <= 0 {
		return fmt.Errorf("pattern timeout must be positive, got: %v", c.PatternTimeout)
	}

	if c.EventBufferSize <= 0 {
		return fmt.Errorf("event buffer size must be positive, got: %d", c.EventBufferSize)
	}

	if c.ResultBufferSize <= 0 {
		return fmt.Errorf("result buffer size must be positive, got: %d", c.ResultBufferSize)
	}

	if c.DefaultQueryLimit <= 0 {
		return fmt.Errorf("default query limit must be positive, got: %d", c.DefaultQueryLimit)
	}

	if c.MaxQueryLimit < c.DefaultQueryLimit {
		return fmt.Errorf("max query limit (%d) must be >= default query limit (%d)", c.MaxQueryLimit, c.DefaultQueryLimit)
	}

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
}

// DefaultTemporalConfig returns production temporal configuration
func DefaultTemporalConfig() *TemporalConfig {
	return &TemporalConfig{
		MaxPatternsTracked: MaxPatternsTracked,
		PatternTimeout:     DefaultPatternTimeout,
		MaxTemporalItems:   MaxTemporalItems,
	}
}

// TestTemporalConfig returns test temporal configuration
func TestTemporalConfig() *TemporalConfig {
	return &TemporalConfig{
		MaxPatternsTracked: MaxPatternsTracked,
		PatternTimeout:     ShortTestTimeout,
		MaxTemporalItems:   TestTemporalItems,
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
