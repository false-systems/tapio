package bpf_common

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cilium/ebpf"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

// SamplingStrategy defines different sampling approaches
type SamplingStrategy int

const (
	// SamplingStrategyUniform applies uniform random sampling
	SamplingStrategyUniform SamplingStrategy = iota
	// SamplingStrategyAdaptive adjusts sampling rate based on load
	SamplingStrategyAdaptive
	// SamplingStrategyReservoir uses reservoir sampling for fixed-size samples
	SamplingStrategyReservoir
	// SamplingStrategyTailBased samples based on trace characteristics
	SamplingStrategyTailBased
	// SamplingStrategyPriority samples based on event priority
	SamplingStrategyPriority
)

// SamplingConfig defines sampling configuration
type SamplingConfig struct {
	// Base sampling rate (0.0 to 1.0)
	BaseRate float64 `json:"base_rate"`

	// Strategy to use
	Strategy SamplingStrategy `json:"strategy"`

	// Adaptive sampling parameters
	AdaptiveMinRate    float64       `json:"adaptive_min_rate"`
	AdaptiveMaxRate    float64       `json:"adaptive_max_rate"`
	AdaptiveTargetEPS  uint64        `json:"adaptive_target_eps"` // Target events per second
	AdaptiveWindowSize time.Duration `json:"adaptive_window_size"`
	AdaptiveAdjustRate float64       `json:"adaptive_adjust_rate"`

	// Reservoir sampling parameters
	ReservoirSize     uint32 `json:"reservoir_size"`
	ReservoirWindowMs uint64 `json:"reservoir_window_ms"`

	// Tail-based sampling parameters
	TailLatencyThresholdMs uint64  `json:"tail_latency_threshold_ms"`
	TailErrorSampleRate    float64 `json:"tail_error_sample_rate"`
	TailSlowSampleRate     float64 `json:"tail_slow_sample_rate"`

	// Priority sampling parameters
	PriorityThreshold uint32  `json:"priority_threshold"`
	PriorityBoostRate float64 `json:"priority_boost_rate"`

	// Per-event-type sampling rates
	EventTypeRates map[string]float64 `json:"event_type_rates"`

	// Statistical guarantees
	ConfidenceLevel float64 `json:"confidence_level"` // e.g., 0.95 for 95%
	MarginOfError   float64 `json:"margin_of_error"`  // e.g., 0.05 for 5%
	MinSampleSize   uint32  `json:"min_sample_size"`
}

// DefaultSamplingConfig returns a default sampling configuration
func DefaultSamplingConfig() *SamplingConfig {
	return &SamplingConfig{
		BaseRate:               0.1, // 10% sampling by default
		Strategy:               SamplingStrategyAdaptive,
		AdaptiveMinRate:        0.01,  // 1% minimum
		AdaptiveMaxRate:        1.0,   // 100% maximum
		AdaptiveTargetEPS:      10000, // Target 10K events/sec
		AdaptiveWindowSize:     5 * time.Second,
		AdaptiveAdjustRate:     0.1,
		ReservoirSize:          1000,
		ReservoirWindowMs:      1000,
		TailLatencyThresholdMs: 100,
		TailErrorSampleRate:    1.0, // Sample all errors
		TailSlowSampleRate:     0.5, // Sample 50% of slow requests
		PriorityThreshold:      5,
		PriorityBoostRate:      2.0,
		EventTypeRates:         make(map[string]float64),
		ConfidenceLevel:        0.95,
		MarginOfError:          0.05,
		MinSampleSize:          100,
	}
}

// SamplingManager manages eBPF-based sampling
type SamplingManager struct {
	mu     sync.RWMutex
	logger *zap.Logger
	config *SamplingConfig

	// OTEL instrumentation
	meter         metric.Meter
	sampledEvents metric.Int64Counter
	droppedEvents metric.Int64Counter
	samplingRate  metric.Float64Gauge

	// eBPF maps
	configMap    *ebpf.Map
	reservoirMap *ebpf.Map
	statsMap     *ebpf.Map

	// Runtime state
	currentRate  float64
	eventCount   uint64
	sampledCount uint64
	windowStart  time.Time

	// Adaptive sampling state
	adaptiveHistory []uint64
	adaptiveIndex   int

	// Reservoir state
	reservoir      []interface{}
	reservoirCount uint64

	// Statistics
	stats *SamplingStatistics
}

// SamplingStatistics tracks sampling performance
type SamplingStatistics struct {
	TotalEvents         uint64    `json:"total_events"`
	SampledEvents       uint64    `json:"sampled_events"`
	DroppedEvents       uint64    `json:"dropped_events"`
	CurrentSampleRate   float64   `json:"current_sample_rate"`
	EffectiveSampleRate float64   `json:"effective_sample_rate"`
	EventsPerSecond     float64   `json:"events_per_second"`
	BytesPerSecond      uint64    `json:"bytes_per_second"`
	LastAdjustment      time.Time `json:"last_adjustment"`
	AdjustmentCount     uint64    `json:"adjustment_count"`

	// Per-strategy statistics
	AdaptiveWindowEPS  float64 `json:"adaptive_window_eps"`
	ReservoirFillRatio float64 `json:"reservoir_fill_ratio"`
	TailSlowEvents     uint64  `json:"tail_slow_events"`
	TailErrorEvents    uint64  `json:"tail_error_events"`
	PriorityHighEvents uint64  `json:"priority_high_events"`

	// Statistical measures
	SampleVariance     float64    `json:"sample_variance"`
	StandardError      float64    `json:"standard_error"`
	ConfidenceInterval [2]float64 `json:"confidence_interval"`
}

// NewSamplingManager creates a new sampling manager
func NewSamplingManager(logger *zap.Logger, config *SamplingConfig, configMap, reservoirMap, statsMap *ebpf.Map) (*SamplingManager, error) {
	if logger == nil {
		var err error
		logger, err = zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("failed to create logger: %w", err)
		}
	}

	if config == nil {
		config = DefaultSamplingConfig()
	}

	// Validate configuration
	if config.BaseRate < 0 || config.BaseRate > 1 {
		return nil, fmt.Errorf("base_rate must be between 0.0 and 1.0")
	}

	meter := otel.Meter("tapio.bpf.sampling")

	sampledEvents, err := meter.Int64Counter(
		"bpf_sampled_events_total",
		metric.WithDescription("Total sampled events"),
	)
	if err != nil {
		logger.Warn("Failed to create sampled_events metric", zap.Error(err))
	}

	droppedEvents, err := meter.Int64Counter(
		"bpf_dropped_events_total",
		metric.WithDescription("Total dropped events due to sampling"),
	)
	if err != nil {
		logger.Warn("Failed to create dropped_events metric", zap.Error(err))
	}

	samplingRate, err := meter.Float64Gauge(
		"bpf_sampling_rate",
		metric.WithDescription("Current sampling rate"),
	)
	if err != nil {
		logger.Warn("Failed to create sampling_rate metric", zap.Error(err))
	}

	sm := &SamplingManager{
		logger:          logger,
		config:          config,
		meter:           meter,
		sampledEvents:   sampledEvents,
		droppedEvents:   droppedEvents,
		samplingRate:    samplingRate,
		configMap:       configMap,
		reservoirMap:    reservoirMap,
		statsMap:        statsMap,
		currentRate:     config.BaseRate,
		windowStart:     time.Now(),
		adaptiveHistory: make([]uint64, 10),
		reservoir:       make([]interface{}, 0, config.ReservoirSize),
		stats:           &SamplingStatistics{},
	}

	// Initialize eBPF map with configuration
	if err := sm.updateBPFConfig(); err != nil {
		return nil, fmt.Errorf("failed to update BPF config: %w", err)
	}

	return sm, nil
}

// updateBPFConfig updates the eBPF map with current configuration
func (sm *SamplingManager) updateBPFConfig() error {
	if sm.configMap == nil {
		return nil
	}

	// Convert Go config to eBPF-compatible structure
	bpfConfig := struct {
		SampleRate        uint32
		Strategy          uint32
		ReservoirSize     uint32
		ReservoirWindowMs uint64
		LatencyThreshold  uint64
		PriorityThreshold uint32
	}{
		SampleRate:        uint32(sm.currentRate * 100), // Convert to percentage
		Strategy:          uint32(sm.config.Strategy),
		ReservoirSize:     sm.config.ReservoirSize,
		ReservoirWindowMs: sm.config.ReservoirWindowMs,
		LatencyThreshold:  sm.config.TailLatencyThresholdMs,
		PriorityThreshold: sm.config.PriorityThreshold,
	}

	key := uint32(0)
	if err := sm.configMap.Update(key, &bpfConfig, ebpf.UpdateAny); err != nil {
		return fmt.Errorf("failed to update sampling config: %w", err)
	}

	return nil
}

// Start begins the sampling manager
func (sm *SamplingManager) Start(ctx context.Context) error {
	// Start adaptive rate adjuster if enabled
	if sm.config.Strategy == SamplingStrategyAdaptive {
		go sm.adaptiveRateAdjuster(ctx)
	}

	// Start statistics updater
	go sm.statsUpdater(ctx)

	sm.logger.Info("Sampling manager started",
		zap.String("strategy", sm.strategyName()),
		zap.Float64("base_rate", sm.config.BaseRate),
	)

	return nil
}

// ShouldSample determines if an event should be sampled
func (sm *SamplingManager) ShouldSample(eventType string, priority uint32, latencyMs uint64, hasError bool) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	atomic.AddUint64(&sm.eventCount, 1)

	sampled := false

	switch sm.config.Strategy {
	case SamplingStrategyUniform:
		sampled = sm.uniformSample()

	case SamplingStrategyAdaptive:
		sampled = sm.adaptiveSample()

	case SamplingStrategyReservoir:
		sampled = sm.reservoirSample()

	case SamplingStrategyTailBased:
		sampled = sm.tailBasedSample(latencyMs, hasError)

	case SamplingStrategyPriority:
		sampled = sm.prioritySample(priority)

	default:
		sampled = sm.uniformSample()
	}

	// Check event-type specific rate
	if !sampled && sm.config.EventTypeRates != nil {
		if rate, exists := sm.config.EventTypeRates[eventType]; exists {
			sampled = sm.randomSample(rate)
		}
	}

	// Update counters
	if sampled {
		atomic.AddUint64(&sm.sampledCount, 1)
		if sm.sampledEvents != nil {
			sm.sampledEvents.Add(context.Background(), 1, metric.WithAttributes(
				attribute.String("event_type", eventType),
				attribute.String("strategy", sm.strategyName()),
			))
		}
	} else {
		atomic.AddUint64(&sm.stats.DroppedEvents, 1)
		if sm.droppedEvents != nil {
			sm.droppedEvents.Add(context.Background(), 1, metric.WithAttributes(
				attribute.String("event_type", eventType),
				attribute.String("strategy", sm.strategyName()),
			))
		}
	}

	return sampled
}

// uniformSample implements uniform random sampling
func (sm *SamplingManager) uniformSample() bool {
	return sm.randomSample(sm.currentRate)
}

// adaptiveSample implements adaptive sampling based on load
func (sm *SamplingManager) adaptiveSample() bool {
	// Use current adaptive rate
	return sm.randomSample(sm.currentRate)
}

// reservoirSample implements reservoir sampling
func (sm *SamplingManager) reservoirSample() bool {
	sm.reservoirCount++

	if uint32(len(sm.reservoir)) < sm.config.ReservoirSize {
		// Reservoir not full, always sample
		return true
	}

	// Reservoir full, use probability
	probability := float64(sm.config.ReservoirSize) / float64(sm.reservoirCount)
	return sm.randomSample(probability)
}

// tailBasedSample implements tail-based sampling
func (sm *SamplingManager) tailBasedSample(latencyMs uint64, hasError bool) bool {
	// Always sample errors
	if hasError {
		atomic.AddUint64(&sm.stats.TailErrorEvents, 1)
		return sm.randomSample(sm.config.TailErrorSampleRate)
	}

	// Sample slow requests
	if latencyMs > sm.config.TailLatencyThresholdMs {
		atomic.AddUint64(&sm.stats.TailSlowEvents, 1)
		return sm.randomSample(sm.config.TailSlowSampleRate)
	}

	// Otherwise use base rate
	return sm.randomSample(sm.currentRate)
}

// prioritySample implements priority-based sampling
func (sm *SamplingManager) prioritySample(priority uint32) bool {
	if priority >= sm.config.PriorityThreshold {
		atomic.AddUint64(&sm.stats.PriorityHighEvents, 1)
		// Boost sampling rate for high priority events
		return sm.randomSample(math.Min(sm.currentRate*sm.config.PriorityBoostRate, 1.0))
	}

	return sm.randomSample(sm.currentRate)
}

// randomSample performs random sampling with given rate
func (sm *SamplingManager) randomSample(rate float64) bool {
	// Simple random sampling
	// In production, use a better random source
	return (uint64(time.Now().UnixNano()) % 100) < uint64(rate*100)
}

// adaptiveRateAdjuster adjusts sampling rate based on load
func (sm *SamplingManager) adaptiveRateAdjuster(ctx context.Context) {
	ticker := time.NewTicker(sm.config.AdaptiveWindowSize)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sm.adjustAdaptiveRate()
		}
	}
}

// adjustAdaptiveRate calculates and sets new adaptive rate
func (sm *SamplingManager) adjustAdaptiveRate() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Calculate events per second in window
	windowDuration := time.Since(sm.windowStart)
	if windowDuration == 0 {
		return
	}

	eps := float64(sm.eventCount) / windowDuration.Seconds()
	sm.stats.AdaptiveWindowEPS = eps

	// Store in history
	sm.adaptiveHistory[sm.adaptiveIndex] = uint64(eps)
	sm.adaptiveIndex = (sm.adaptiveIndex + 1) % len(sm.adaptiveHistory)

	// Calculate average EPS from history
	var totalEPS uint64
	var count int
	for _, e := range sm.adaptiveHistory {
		if e > 0 {
			totalEPS += e
			count++
		}
	}

	if count == 0 {
		return
	}

	avgEPS := float64(totalEPS) / float64(count)

	// Adjust rate based on target EPS
	targetEPS := float64(sm.config.AdaptiveTargetEPS)

	if avgEPS > targetEPS {
		// Too many events, reduce sampling rate
		reductionFactor := targetEPS / avgEPS
		sm.currentRate *= reductionFactor
	} else if avgEPS < targetEPS*0.8 {
		// Too few events, increase sampling rate
		increaseFactor := 1.0 + sm.config.AdaptiveAdjustRate
		sm.currentRate *= increaseFactor
	}

	// Enforce bounds
	if sm.currentRate < sm.config.AdaptiveMinRate {
		sm.currentRate = sm.config.AdaptiveMinRate
	}
	if sm.currentRate > sm.config.AdaptiveMaxRate {
		sm.currentRate = sm.config.AdaptiveMaxRate
	}

	// Update BPF config
	sm.updateBPFConfig()

	// Update metrics
	if sm.samplingRate != nil {
		sm.samplingRate.Record(context.Background(), sm.currentRate, metric.WithAttributes(
			attribute.String("strategy", "adaptive"),
		))
	}

	// Reset counters
	sm.eventCount = 0
	sm.sampledCount = 0
	sm.windowStart = time.Now()
	sm.stats.LastAdjustment = time.Now()
	sm.stats.AdjustmentCount++

	sm.logger.Debug("Adjusted adaptive sampling rate",
		zap.Float64("old_rate", sm.stats.CurrentSampleRate),
		zap.Float64("new_rate", sm.currentRate),
		zap.Float64("avg_eps", avgEPS),
		zap.Float64("target_eps", targetEPS),
	)

	sm.stats.CurrentSampleRate = sm.currentRate
}

// statsUpdater periodically updates statistics
func (sm *SamplingManager) statsUpdater(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sm.updateStatistics()
		}
	}
}

// updateStatistics calculates and updates statistics
func (sm *SamplingManager) updateStatistics() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.stats.TotalEvents = sm.eventCount
	sm.stats.SampledEvents = sm.sampledCount

	if sm.eventCount > 0 {
		sm.stats.EffectiveSampleRate = float64(sm.sampledCount) / float64(sm.eventCount)
	}

	// Calculate statistical measures
	sm.calculateStatisticalMeasures()

	// Update reservoir fill ratio
	if sm.config.Strategy == SamplingStrategyReservoir {
		sm.stats.ReservoirFillRatio = float64(len(sm.reservoir)) / float64(sm.config.ReservoirSize)
	}
}

// calculateStatisticalMeasures calculates variance, standard error, and confidence intervals
func (sm *SamplingManager) calculateStatisticalMeasures() {
	n := float64(sm.sampledCount)
	if n < float64(sm.config.MinSampleSize) {
		return
	}

	// Estimate population size (if known)
	N := float64(sm.eventCount)

	// Sample proportion
	p := sm.stats.EffectiveSampleRate

	// Sample variance for proportion
	variance := p * (1 - p) / n

	// Finite population correction
	if N > 0 {
		fpc := (N - n) / (N - 1)
		variance *= fpc
	}

	sm.stats.SampleVariance = variance
	sm.stats.StandardError = math.Sqrt(variance)

	// Calculate confidence interval (using normal approximation)
	zScore := sm.getZScore(sm.config.ConfidenceLevel)
	margin := zScore * sm.stats.StandardError

	sm.stats.ConfidenceInterval[0] = math.Max(0, p-margin)
	sm.stats.ConfidenceInterval[1] = math.Min(1, p+margin)
}

// getZScore returns the z-score for a given confidence level
func (sm *SamplingManager) getZScore(confidenceLevel float64) float64 {
	// Common z-scores
	switch confidenceLevel {
	case 0.90:
		return 1.645
	case 0.95:
		return 1.96
	case 0.99:
		return 2.576
	default:
		return 1.96 // Default to 95%
	}
}

// GetStatistics returns current sampling statistics
func (sm *SamplingManager) GetStatistics() *SamplingStatistics {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stats := *sm.stats
	stats.CurrentSampleRate = sm.currentRate

	return &stats
}

// SetSamplingRate manually sets the sampling rate
func (sm *SamplingManager) SetSamplingRate(rate float64) error {
	if rate < 0 || rate > 1 {
		return fmt.Errorf("sampling rate must be between 0.0 and 1.0")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.currentRate = rate
	sm.config.BaseRate = rate

	// Update BPF config
	if err := sm.updateBPFConfig(); err != nil {
		return fmt.Errorf("failed to update BPF config: %w", err)
	}

	// Update metric
	if sm.samplingRate != nil {
		sm.samplingRate.Record(context.Background(), rate, metric.WithAttributes(
			attribute.String("strategy", sm.strategyName()),
		))
	}

	sm.logger.Info("Sampling rate updated",
		zap.Float64("new_rate", rate),
	)

	return nil
}

// SetStrategy changes the sampling strategy
func (sm *SamplingManager) SetStrategy(strategy SamplingStrategy) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.config.Strategy = strategy

	// Reset strategy-specific state
	switch strategy {
	case SamplingStrategyReservoir:
		sm.reservoir = make([]interface{}, 0, sm.config.ReservoirSize)
		sm.reservoirCount = 0
	case SamplingStrategyAdaptive:
		sm.adaptiveHistory = make([]uint64, 10)
		sm.adaptiveIndex = 0
	}

	// Update BPF config
	if err := sm.updateBPFConfig(); err != nil {
		return fmt.Errorf("failed to update BPF config: %w", err)
	}

	sm.logger.Info("Sampling strategy updated",
		zap.String("new_strategy", sm.strategyName()),
	)

	return nil
}

// SetEventTypeRate sets sampling rate for a specific event type
func (sm *SamplingManager) SetEventTypeRate(eventType string, rate float64) error {
	if rate < 0 || rate > 1 {
		return fmt.Errorf("sampling rate must be between 0.0 and 1.0")
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.config.EventTypeRates == nil {
		sm.config.EventTypeRates = make(map[string]float64)
	}

	sm.config.EventTypeRates[eventType] = rate

	sm.logger.Info("Event type sampling rate updated",
		zap.String("event_type", eventType),
		zap.Float64("rate", rate),
	)

	return nil
}

// CalculateRequiredSampleSize calculates the required sample size for statistical validity
func (sm *SamplingManager) CalculateRequiredSampleSize(populationSize uint64) uint64 {
	// Using Cochran's formula for sample size
	z := sm.getZScore(sm.config.ConfidenceLevel)
	e := sm.config.MarginOfError
	p := 0.5 // Use 0.5 for maximum sample size

	// n = (z^2 * p * (1-p)) / e^2
	n := (z * z * p * (1 - p)) / (e * e)

	// Apply finite population correction if population is known
	if populationSize > 0 {
		N := float64(populationSize)
		n = n / (1 + (n-1)/N)
	}

	// Ensure minimum sample size
	if n < float64(sm.config.MinSampleSize) {
		n = float64(sm.config.MinSampleSize)
	}

	return uint64(math.Ceil(n))
}

// strategyName returns the name of the current strategy
func (sm *SamplingManager) strategyName() string {
	switch sm.config.Strategy {
	case SamplingStrategyUniform:
		return "uniform"
	case SamplingStrategyAdaptive:
		return "adaptive"
	case SamplingStrategyReservoir:
		return "reservoir"
	case SamplingStrategyTailBased:
		return "tail_based"
	case SamplingStrategyPriority:
		return "priority"
	default:
		return "unknown"
	}
}
