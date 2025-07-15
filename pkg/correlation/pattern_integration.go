package correlation

import (
	"context"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation/patterns"
	"github.com/yairfalse/tapio/pkg/correlation/types"
	"github.com/yairfalse/tapio/pkg/events/opinionated"
)

// PatternIntegratedEngine extends PerfectEngine with ML-based pattern detection
type PatternIntegratedEngine struct {
	*PerfectEngine

	// Pattern detection components
	patternRegistry  types.PatternRegistry
	patternValidator types.PatternValidator
	patternConfig    types.PatternConfig

	// Integration state
	enablePatterns   bool
	patternResults   chan *types.PatternResult
	integrationStats *PatternIntegrationStats

	// Pattern-specific caches
	patternCache *PatternResultCache

	// Configuration
	integrationConfig *PatternIntegrationConfig
}

// PatternIntegrationConfig configures pattern integration
type PatternIntegrationConfig struct {
	// Pattern detection settings
	EnablePatternDetection   bool          `json:"enable_pattern_detection"`
	PatternDetectionInterval time.Duration `json:"pattern_detection_interval"`
	PatternBufferSize        int           `json:"pattern_buffer_size"`

	// Integration behavior
	MergeWithCorrelations   bool    `json:"merge_with_correlations"`
	PrioritizePatterns      bool    `json:"prioritize_patterns"`
	PatternConfidenceWeight float64 `json:"pattern_confidence_weight"`

	// Validation settings
	EnableValidation   bool          `json:"enable_validation"`
	ValidationInterval time.Duration `json:"validation_interval"`

	// Performance settings
	MaxConcurrentPatterns    int           `json:"max_concurrent_patterns"`
	PatternProcessingTimeout time.Duration `json:"pattern_processing_timeout"`

	// Pattern-specific configurations
	MemoryLeakConfig        types.PatternConfig `json:"memory_leak_config"`
	NetworkFailureConfig    types.PatternConfig `json:"network_failure_config"`
	StorageBottleneckConfig types.PatternConfig `json:"storage_bottleneck_config"`
	RuntimeFailureConfig    types.PatternConfig `json:"runtime_failure_config"`
	DependencyFailureConfig types.PatternConfig `json:"dependency_failure_config"`
}

// PatternIntegrationStats tracks pattern integration performance
type PatternIntegrationStats struct {
	// Pattern detection metrics
	PatternsDetected  uint64 `json:"patterns_detected"`
	PatternsFused     uint64 `json:"patterns_fused"`
	PatternsValidated uint64 `json:"patterns_validated"`

	// Integration performance
	AvgPatternProcessingTime   time.Duration `json:"avg_pattern_processing_time"`
	AvgIntegrationTime         time.Duration `json:"avg_integration_time"`
	PatternDetectionThroughput float64       `json:"pattern_detection_throughput"`

	// Pattern type breakdown
	MemoryLeakPatterns        uint64 `json:"memory_leak_patterns"`
	NetworkFailurePatterns    uint64 `json:"network_failure_patterns"`
	StorageBottleneckPatterns uint64 `json:"storage_bottleneck_patterns"`
	RuntimeFailurePatterns    uint64 `json:"runtime_failure_patterns"`
	DependencyFailurePatterns uint64 `json:"dependency_failure_patterns"`

	// Quality metrics
	OverallAccuracy   float64 `json:"overall_accuracy"`
	FalsePositiveRate float64 `json:"false_positive_rate"`
	FalseNegativeRate float64 `json:"false_negative_rate"`

	mutex sync.RWMutex
}

// PatternResultCache caches pattern detection results for efficiency
type PatternResultCache struct {
	cache   map[string]*CachedPatternResult
	maxSize int
	ttl     time.Duration
	mutex   sync.RWMutex
}

// CachedPatternResult represents a cached pattern detection result
type CachedPatternResult struct {
	Result      *types.PatternResult
	CachedAt    time.Time
	AccessCount int64
	LastAccess  time.Time
}

// NewPatternIntegratedEngine creates a new pattern-integrated correlation engine
func NewPatternIntegratedEngine(config *PerfectConfig, integrationConfig *PatternIntegrationConfig) (*PatternIntegratedEngine, error) {
	// Create base perfect engine
	perfectEngine, err := NewPerfectEngine(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create perfect engine: %w", err)
	}

	// Set default integration config if not provided
	if integrationConfig == nil {
		integrationConfig = DefaultPatternIntegrationConfig()
	}

	// Create pattern-integrated engine
	engine := &PatternIntegratedEngine{
		PerfectEngine:     perfectEngine,
		enablePatterns:    integrationConfig.EnablePatternDetection,
		integrationConfig: integrationConfig,
		integrationStats:  &PatternIntegrationStats{},
		patternResults:    make(chan *types.PatternResult, integrationConfig.PatternBufferSize),
	}

	// Initialize pattern detection if enabled
	if integrationConfig.EnablePatternDetection {
		// Create pattern registry with all detectors
		engine.patternRegistry = patterns.NewPatternRegistry()

		// Configure individual pattern detectors
		engine.configurePatternDetectors()

		// Initialize pattern validator if enabled
		if integrationConfig.EnableValidation {
			engine.patternValidator = patterns.NewPatternValidator(patterns.DefaultPatternConfig())
		}

		// Initialize pattern cache
		engine.patternCache = NewPatternResultCache(1000, 5*time.Minute)
	}

	return engine, nil
}

// DefaultPatternIntegrationConfig returns default integration configuration
func DefaultPatternIntegrationConfig() *PatternIntegrationConfig {
	defaultPatternConfig := patterns.DefaultPatternConfig()

	return &PatternIntegrationConfig{
		EnablePatternDetection:   true,
		PatternDetectionInterval: 30 * time.Second,
		PatternBufferSize:        100,
		MergeWithCorrelations:    true,
		PrioritizePatterns:       true,
		PatternConfidenceWeight:  0.8,
		EnableValidation:         true,
		ValidationInterval:       1 * time.Hour,
		MaxConcurrentPatterns:    4,
		PatternProcessingTimeout: 10 * time.Second,

		// Pattern-specific configs (can be customized per pattern)
		MemoryLeakConfig:        defaultPatternConfig,
		NetworkFailureConfig:    defaultPatternConfig,
		StorageBottleneckConfig: defaultPatternConfig,
		RuntimeFailureConfig:    defaultPatternConfig,
		DependencyFailureConfig: defaultPatternConfig,
	}
}

// configurePatternDetectors configures individual pattern detectors
func (pie *PatternIntegratedEngine) configurePatternDetectors() {
	if pie.patternRegistry == nil {
		return
	}

	// Note: Pattern detectors would be configured here once they implement
	// the types.PatternDetector interface properly
}

// ProcessOpinionatedEventWithPatterns processes an event through both correlation and pattern detection
func (pie *PatternIntegratedEngine) ProcessOpinionatedEventWithPatterns(ctx context.Context, event *opinionated.OpinionatedEvent) (*IntegratedResult, error) {
	start := time.Now()

	// Process through original correlation engine
	err := pie.PerfectEngine.ProcessOpinionatedEvent(ctx, event)
	if err != nil {
		return nil, fmt.Errorf("perfect engine processing failed: %w", err)
	}

	result := &IntegratedResult{
		Event:              event,
		ProcessingTime:     time.Since(start),
		CorrelationResults: []*Correlation{}, // Would extract from perfect engine
		PatternResults:     []*types.PatternResult{},
		FusedInsights:      []*FusedInsight{},
	}

	// Run pattern detection if enabled
	if pie.enablePatterns {
		patternResults, err := pie.runPatternDetection(ctx, event)
		if err != nil {
			// Log error but continue with correlation results
			result.PatternError = err.Error()
		} else {
			result.PatternResults = patternResults

			// Update pattern-specific stats
			pie.updatePatternStats(patternResults)

			// Fuse correlation and pattern results
			fusedInsights := pie.fuseResults(result.CorrelationResults, patternResults)
			result.FusedInsights = fusedInsights
		}
	}

	result.ProcessingTime = time.Since(start)

	// Update integration stats
	pie.updateIntegrationStats(result)

	return result, nil
}

// runPatternDetection executes pattern detection for an event
func (pie *PatternIntegratedEngine) runPatternDetection(ctx context.Context, event *opinionated.OpinionatedEvent) ([]*types.PatternResult, error) {
	// Check cache first
	cacheKey := pie.generateCacheKey(event)
	if cachedResult := pie.patternCache.Get(cacheKey); cachedResult != nil {
		return []*types.PatternResult{cachedResult}, nil
	}

	// Convert opinionated event to correlation events for pattern detection
	correlationEvents := pie.convertToCorrelationEvents(event)

	// Extract metrics from event (simplified - would integrate with metrics store)
	metrics := pie.extractMetricsFromOpinionatedEvent(event)

	// Run pattern detection with timeout
	patternCtx, cancel := context.WithTimeout(ctx, pie.integrationConfig.PatternProcessingTimeout)
	defer cancel()

	patternResults, err := pie.patternRegistry.DetectAll(patternCtx, correlationEvents, metrics)
	if err != nil {
		return nil, fmt.Errorf("pattern detection failed: %w", err)
	}

	// Filter by confidence and cache results
	var filteredResults []*types.PatternResult
	for i := range patternResults {
		result := &patternResults[i]
		if result.Confidence >= pie.integrationConfig.PatternConfidenceWeight {
			filteredResults = append(filteredResults, result)

			// Cache successful detection
			pie.patternCache.Put(cacheKey, result)
		}
	}

	return filteredResults, nil
}

// fuseResults combines correlation and pattern results into unified insights
func (pie *PatternIntegratedEngine) fuseResults(correlations []*Correlation, patterns []*types.PatternResult) []*FusedInsight {
	var fusedInsights []*FusedInsight

	// Create pattern-driven insights
	for _, pattern := range patterns {
		insight := &FusedInsight{
			Type:             "pattern_driven",
			PatternID:        pattern.PatternID,
			PatternName:      pattern.PatternName,
			Confidence:       pattern.Confidence,
			Severity:         pattern.Severity,
			Description:      pie.generatePatternDescription(pattern),
			RootCause:        pie.extractRootCause(pattern),
			Predictions:      pie.convertPatternPredictions(pattern),
			Recommendations:  pie.convertPatternRecommendations(pattern),
			AffectedEntities: pattern.AffectedEntity,
			DetectionTime:    pattern.DetectedAt,

			// Integration metadata
			CorrelatedEvents: pie.findCorrelatedEvents(pattern, correlations),
			FusionConfidence: pie.calculateFusionConfidence(pattern, correlations),
		}

		fusedInsights = append(fusedInsights, insight)
	}

	// Create correlation-enhanced insights
	for _, correlation := range correlations {
		// Check if this correlation enhances any pattern
		enhancedPattern := pie.findEnhancingPattern(correlation, patterns)
		if enhancedPattern != nil {
			insight := &FusedInsight{
				Type:             "correlation_enhanced",
				CorrelationType:  correlation.Type,
				Confidence:       correlation.Confidence,
				Description:      pie.generateCorrelationDescription(correlation),
				EnhancedPattern:  enhancedPattern,
				FusionConfidence: pie.calculateFusionConfidence(enhancedPattern, []*Correlation{correlation}),
			}
			fusedInsights = append(fusedInsights, insight)
		}
	}

	return fusedInsights
}

// Start starts the pattern-integrated engine
func (pie *PatternIntegratedEngine) Start(ctx context.Context) error {
	// Start base perfect engine
	if err := pie.PerfectEngine.Start(ctx); err != nil {
		return fmt.Errorf("failed to start perfect engine: %w", err)
	}

	// Start pattern detection if enabled
	if pie.enablePatterns {
		go pie.runContinuousPatternDetection(ctx)

		// Start pattern validation if enabled
		if pie.patternValidator != nil {
			go pie.runPeriodicValidation(ctx)
		}
	}

	return nil
}

// runContinuousPatternDetection runs pattern detection continuously
func (pie *PatternIntegratedEngine) runContinuousPatternDetection(ctx context.Context) {
	ticker := time.NewTicker(pie.integrationConfig.PatternDetectionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Get recent events from event store
			events := pie.getRecentEventsForPatternDetection()

			if len(events) > 0 {
				// Extract metrics
				metrics := pie.extractMetricsFromEvents(events)

				// Run pattern detection
				patternResults, err := pie.patternRegistry.DetectAll(ctx, events, metrics)
				if err != nil {
					continue
				}

				// Send results to channel for processing
				for i := range patternResults {
					result := &patternResults[i]
					select {
					case pie.patternResults <- result:
						atomic.AddUint64(&pie.integrationStats.PatternsDetected, 1)
					default:
						// Channel full, skip
					}
				}
			}

		case <-ctx.Done():
			return
		}
	}
}

// runPeriodicValidation runs pattern validation periodically
func (pie *PatternIntegratedEngine) runPeriodicValidation(ctx context.Context) {
	ticker := time.NewTicker(pie.integrationConfig.ValidationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Run validation for recent pattern results
			// This would validate against ground truth data
			atomic.AddUint64(&pie.integrationStats.PatternsValidated, 1)

		case <-ctx.Done():
			return
		}
	}
}

// GetIntegratedStats returns comprehensive statistics including pattern integration
func (pie *PatternIntegratedEngine) GetIntegratedStats() *IntegratedEngineStats {
	baseStats := pie.PerfectEngine.GetStats()

	pie.integrationStats.mutex.RLock()
	patternStats := *pie.integrationStats
	pie.integrationStats.mutex.RUnlock()

	return &IntegratedEngineStats{
		PerfectEngineStats:      baseStats,
		PatternIntegrationStats: &patternStats,

		// Additional integration metrics
		PatternDetectionEnabled:  pie.enablePatterns,
		PatternValidationEnabled: pie.patternValidator != nil,
		PatternCacheStats:        pie.patternCache.GetStats(),

		TotalInsights: baseStats.InsightsGenerated + patternStats.PatternsFused,
	}
}

// Supporting types for integration

// IntegratedResult represents the result of integrated processing
type IntegratedResult struct {
	Event          *opinionated.OpinionatedEvent `json:"event"`
	ProcessingTime time.Duration                 `json:"processing_time"`

	// Results from different engines
	CorrelationResults []*Correlation         `json:"correlation_results"`
	PatternResults     []*types.PatternResult `json:"pattern_results"`

	// Fused insights
	FusedInsights []*FusedInsight `json:"fused_insights"`

	// Error handling
	PatternError string `json:"pattern_error,omitempty"`
}

// FusedInsight represents an insight created by fusing correlation and pattern data
type FusedInsight struct {
	Type string `json:"type"` // "pattern_driven", "correlation_enhanced"

	// Pattern information
	PatternID   string `json:"pattern_id,omitempty"`
	PatternName string `json:"pattern_name,omitempty"`

	// Correlation information
	CorrelationType string `json:"correlation_type,omitempty"`

	// Common fields
	Confidence  float64        `json:"confidence"`
	Severity    types.Severity `json:"severity"`
	Description string         `json:"description"`
	RootCause   string         `json:"root_cause"`

	// Predictions and recommendations
	Predictions     []*FusedPrediction     `json:"predictions"`
	Recommendations []*FusedRecommendation `json:"recommendations"`

	// Affected resources
	AffectedEntities types.Entity `json:"affected_entities"`

	// Timing
	DetectionTime time.Time `json:"detection_time"`

	// Integration metadata
	CorrelatedEvents []*Correlation       `json:"correlated_events"`
	EnhancedPattern  *types.PatternResult `json:"enhanced_pattern,omitempty"`
	FusionConfidence float64              `json:"fusion_confidence"`
}

// FusedPrediction represents a prediction from fused analysis
type FusedPrediction struct {
	Type              string        `json:"type"`
	Description       string        `json:"description"`
	Probability       float64       `json:"probability"`
	TimeToEvent       time.Duration `json:"time_to_event"`
	Confidence        float64       `json:"confidence"`
	PreventionActions []string      `json:"prevention_actions"`
}

// FusedRecommendation represents a recommendation from fused analysis
type FusedRecommendation struct {
	Priority        int           `json:"priority"`
	Category        string        `json:"category"`
	Description     string        `json:"description"`
	Action          string        `json:"action"`
	ExpectedImpact  string        `json:"expected_impact"`
	EstimatedTime   time.Duration `json:"estimated_time"`
	AutomationLevel string        `json:"automation_level"` // "manual", "semi-automated", "automated"
}

// IntegratedEngineStats provides comprehensive statistics
type IntegratedEngineStats struct {
	PerfectEngineStats      *PerfectEngineStats      `json:"perfect_engine_stats"`
	PatternIntegrationStats *PatternIntegrationStats `json:"pattern_integration_stats"`

	PatternDetectionEnabled  bool               `json:"pattern_detection_enabled"`
	PatternValidationEnabled bool               `json:"pattern_validation_enabled"`
	PatternCacheStats        *PatternCacheStats `json:"pattern_cache_stats"`

	TotalInsights uint64 `json:"total_insights"`
}

// PatternCacheStats provides pattern cache statistics
type PatternCacheStats struct {
	CacheSize     int           `json:"cache_size"`
	CacheHits     uint64        `json:"cache_hits"`
	CacheMisses   uint64        `json:"cache_misses"`
	CacheHitRate  float64       `json:"cache_hit_rate"`
	EvictionCount uint64        `json:"eviction_count"`
	AverageAge    time.Duration `json:"average_age"`
}

// Helper methods and implementations

func (pie *PatternIntegratedEngine) updatePatternStats(patterns []*types.PatternResult) {
	pie.integrationStats.mutex.Lock()
	defer pie.integrationStats.mutex.Unlock()

	for _, pattern := range patterns {
		switch pattern.PatternID {
		case "memory_leak_oom_cascade":
			atomic.AddUint64(&pie.integrationStats.MemoryLeakPatterns, 1)
		case "network_failure_cascade":
			atomic.AddUint64(&pie.integrationStats.NetworkFailurePatterns, 1)
		case "storage_io_bottleneck":
			atomic.AddUint64(&pie.integrationStats.StorageBottleneckPatterns, 1)
		case "container_runtime_failure":
			atomic.AddUint64(&pie.integrationStats.RuntimeFailurePatterns, 1)
		case "service_dependency_failure":
			atomic.AddUint64(&pie.integrationStats.DependencyFailurePatterns, 1)
		}
	}
}

func (pie *PatternIntegratedEngine) updateIntegrationStats(result *IntegratedResult) {
	pie.integrationStats.mutex.Lock()
	defer pie.integrationStats.mutex.Unlock()

	if len(result.FusedInsights) > 0 {
		atomic.AddUint64(&pie.integrationStats.PatternsFused, uint64(len(result.FusedInsights)))
	}

	// Update average processing time
	if pie.integrationStats.AvgIntegrationTime == 0 {
		pie.integrationStats.AvgIntegrationTime = result.ProcessingTime
	} else {
		// Exponential moving average
		alpha := 0.1
		pie.integrationStats.AvgIntegrationTime = time.Duration(float64(pie.integrationStats.AvgIntegrationTime)*(1-alpha) + float64(result.ProcessingTime)*alpha)
	}
}

// Placeholder implementations for helper methods
func (pie *PatternIntegratedEngine) generateCacheKey(event *opinionated.OpinionatedEvent) string {
	return fmt.Sprintf("%s-%d", event.Id, event.TimestampNs)
}

func (pie *PatternIntegratedEngine) convertToCorrelationEvents(event *opinionated.OpinionatedEvent) []types.Event {
	// Convert opinionated event to correlation events
	return []types.Event{} // Simplified placeholder
}

func (pie *PatternIntegratedEngine) extractMetricsFromOpinionatedEvent(event *opinionated.OpinionatedEvent) map[string]types.MetricSeries {
	// Extract metrics from opinionated event
	return make(map[string]types.MetricSeries) // Simplified placeholder
}

func (pie *PatternIntegratedEngine) getRecentEventsForPatternDetection() []types.Event {
	// Get recent events from event store
	return []types.Event{} // Simplified placeholder
}

func (pie *PatternIntegratedEngine) extractMetricsFromEvents(events []types.Event) map[string]types.MetricSeries {
	// Extract metrics from events
	return make(map[string]types.MetricSeries) // Simplified placeholder
}

func (pie *PatternIntegratedEngine) generatePatternDescription(pattern *types.PatternResult) string {
	return fmt.Sprintf("Pattern detected: %s with %.1f%% confidence", pattern.PatternName, pattern.Confidence*100)
}

func (pie *PatternIntegratedEngine) extractRootCause(pattern *types.PatternResult) string {
	if pattern.RootCause != "" {
		return pattern.RootCause
	}
	return "unknown"
}

func (pie *PatternIntegratedEngine) convertPatternPredictions(pattern *types.PatternResult) []*FusedPrediction {
	var fusedPredictions []*FusedPrediction
	// Convert from pattern's prediction format
	if pattern.Prediction != "" {
		fusedPredictions = append(fusedPredictions, &FusedPrediction{
			Type:        "pattern_based",
			Description: pattern.Prediction,
			Probability: pattern.Confidence,
			Confidence:  pattern.Confidence,
		})
	}
	return fusedPredictions
}

func (pie *PatternIntegratedEngine) convertPatternRecommendations(pattern *types.PatternResult) []*FusedRecommendation {
	var fusedRecommendations []*FusedRecommendation
	for i, rec := range pattern.Recommendations {
		fusedRecommendations = append(fusedRecommendations, &FusedRecommendation{
			Priority:        i + 1, // Higher priority first
			Category:        string(rec.Priority),
			Description:     rec.Description,
			Action:          rec.Commands[0], // First command as primary action
			ExpectedImpact:  rec.Title,
			AutomationLevel: pie.determineAutomationLevel(rec.AutoApply),
		})
	}
	return fusedRecommendations
}

func (pie *PatternIntegratedEngine) findCorrelatedEvents(pattern *types.PatternResult, correlations []*Correlation) []*Correlation {
	// Find correlations that relate to this pattern
	return []*Correlation{} // Simplified placeholder
}

func (pie *PatternIntegratedEngine) findEnhancingPattern(correlation *Correlation, patterns []*types.PatternResult) *types.PatternResult {
	// Find pattern that this correlation enhances
	return nil // Simplified placeholder
}

func (pie *PatternIntegratedEngine) calculateFusionConfidence(pattern *types.PatternResult, correlations []*Correlation) float64 {
	// Calculate confidence score for fused result
	baseConfidence := pattern.Confidence
	correlationBoost := float64(len(correlations)) * 0.1
	return math.Min(baseConfidence+correlationBoost, 1.0)
}

func (pie *PatternIntegratedEngine) generateCorrelationDescription(correlation *Correlation) string {
	return fmt.Sprintf("Correlation detected: %s", correlation.Type)
}

func (pie *PatternIntegratedEngine) determineAutomationLevel(autoApply bool) string {
	if autoApply {
		return "automated"
	}
	return "manual"
}

// Pattern cache implementation
func NewPatternResultCache(maxSize int, ttl time.Duration) *PatternResultCache {
	return &PatternResultCache{
		cache:   make(map[string]*CachedPatternResult),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

func (prc *PatternResultCache) Get(key string) *types.PatternResult {
	prc.mutex.RLock()
	defer prc.mutex.RUnlock()

	cached, exists := prc.cache[key]
	if !exists {
		return nil
	}

	// Check TTL
	if time.Since(cached.CachedAt) > prc.ttl {
		delete(prc.cache, key)
		return nil
	}

	// Update access statistics
	atomic.AddInt64(&cached.AccessCount, 1)
	cached.LastAccess = time.Now()

	return cached.Result
}

func (prc *PatternResultCache) Put(key string, result *types.PatternResult) {
	prc.mutex.Lock()
	defer prc.mutex.Unlock()

	// Evict old entries if cache is full
	if len(prc.cache) >= prc.maxSize {
		prc.evictOldest()
	}

	prc.cache[key] = &CachedPatternResult{
		Result:     result,
		CachedAt:   time.Now(),
		LastAccess: time.Now(),
	}
}

func (prc *PatternResultCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, cached := range prc.cache {
		if oldestKey == "" || cached.LastAccess.Before(oldestTime) {
			oldestKey = key
			oldestTime = cached.LastAccess
		}
	}

	if oldestKey != "" {
		delete(prc.cache, oldestKey)
	}
}

func (prc *PatternResultCache) GetStats() *PatternCacheStats {
	prc.mutex.RLock()
	defer prc.mutex.RUnlock()

	return &PatternCacheStats{
		CacheSize: len(prc.cache),
		// Other stats would be tracked with atomic counters
	}
}