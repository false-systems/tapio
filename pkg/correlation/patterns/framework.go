package patterns

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation/types"
)

// CausalityNode represents a node in the causality chain
type CausalityNode struct {
	EventID    string         `json:"event_id"`
	EventType  string         `json:"event_type"`
	Entity     types.Entity   `json:"entity"`
	Timestamp  time.Time      `json:"timestamp"`
	Confidence float64        `json:"confidence"`

	// Causality relationships
	CausedBy []string `json:"caused_by,omitempty"`
	Causes   []string `json:"causes,omitempty"`

	// Supporting evidence
	Metrics    map[string]float64     `json:"metrics,omitempty"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`

	// Analysis
	CausalStrength float64       `json:"causal_strength"` // 0.0 to 1.0
	TimeDelay      time.Duration `json:"time_delay"`
}

// PatternMetrics contains quantitative measurements of the pattern
type PatternMetrics struct {
	// Resource metrics
	MemoryPressure     float64 `json:"memory_pressure"`     // 0.0 to 1.0
	CPUUtilization     float64 `json:"cpu_utilization"`     // 0.0 to 1.0
	DiskUtilization    float64 `json:"disk_utilization"`    // 0.0 to 1.0
	NetworkUtilization float64 `json:"network_utilization"` // 0.0 to 1.0

	// Performance metrics
	Latency     time.Duration `json:"latency"`
	Throughput  float64       `json:"throughput"`
	ErrorRate   float64       `json:"error_rate"`   // 0.0 to 1.0
	SuccessRate float64       `json:"success_rate"` // 0.0 to 1.0

	// Temporal metrics
	FrequencyHertz float64       `json:"frequency_hz"`
	PeriodDuration time.Duration `json:"period_duration"`
	TrendSlope     float64       `json:"trend_slope"` // Positive = increasing
	Seasonality    float64       `json:"seasonality"` // 0.0 to 1.0

	// Statistical metrics
	Mean              float64 `json:"mean"`
	StandardDeviation float64 `json:"standard_deviation"`
	Variance          float64 `json:"variance"`
	Skewness          float64 `json:"skewness"`
	Kurtosis          float64 `json:"kurtosis"`

	// Custom pattern-specific metrics
	CustomMetrics map[string]float64 `json:"custom_metrics,omitempty"`
}

// Prediction represents a future state prediction based on the pattern
type Prediction struct {
	Type             string        `json:"type"`
	Description      string        `json:"description"`
	Probability      float64       `json:"probability"` // 0.0 to 1.0
	ExpectedTime     time.Time     `json:"expected_time"`
	TimeWindow       time.Duration `json:"time_window"`
	Confidence       float64       `json:"confidence"` // 0.0 to 1.0
	Impact           string        `json:"impact"`
	PreventionWindow time.Duration `json:"prevention_window"` // Time left to prevent
}

// ImpactAssessment quantifies the business and technical impact
type ImpactAssessment struct {
	// Technical impact
	AffectedServices int `json:"affected_services"`
	AffectedPods     int `json:"affected_pods"`
	AffectedNodes    int `json:"affected_nodes"`

	// Performance impact
	PerformanceDegradation float64 `json:"performance_degradation"` // 0.0 to 1.0
	CapacityReduction      float64 `json:"capacity_reduction"`      // 0.0 to 1.0

	// Business impact (estimated)
	UserImpact        string        `json:"user_impact"`        // "none", "minor", "major", "severe"
	SLAViolationRisk  float64       `json:"sla_violation_risk"` // 0.0 to 1.0
	EstimatedDowntime time.Duration `json:"estimated_downtime"`

	// Financial impact (if available)
	EstimatedCost float64 `json:"estimated_cost,omitempty"`
	Currency      string  `json:"currency,omitempty"`

	// Recovery metrics
	MTTR           time.Duration `json:"mttr"` // Mean Time To Recovery
	MTTRConfidence float64       `json:"mttr_confidence"`
}

// RemediationAction represents an action to resolve the pattern
type RemediationAction struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Priority    int    `json:"priority"` // 1 = highest
	Urgency     string `json:"urgency"`  // "low", "medium", "high", "critical"

	// Execution details
	Command    string            `json:"command,omitempty"`
	Target     types.Entity      `json:"target"`
	Parameters map[string]string `json:"parameters,omitempty"`

	// Safety and validation
	SafetyLevel      string `json:"safety_level"` // "safe", "moderate", "risky"
	RequiresApproval bool   `json:"requires_approval"`
	DryRunSupported  bool   `json:"dry_run_supported"`

	// Expected outcomes
	ExpectedEffect     string        `json:"expected_effect"`
	SuccessProbability float64       `json:"success_probability"` // 0.0 to 1.0
	EstimatedDuration  time.Duration `json:"estimated_duration"`

	// Rollback information
	RollbackSupported bool   `json:"rollback_supported"`
	RollbackCommand   string `json:"rollback_command,omitempty"`
}

// PerformanceMetrics represents performance metrics for pattern detection
type PerformanceMetrics struct {
	AvgLatency      time.Duration `json:"avg_latency"`
	P95Latency      time.Duration `json:"p95_latency"`
	P99Latency      time.Duration `json:"p99_latency"`
	MaxLatency      time.Duration `json:"max_latency"`
	TotalExecutions int64         `json:"total_executions"`
	ErrorCount      int64         `json:"error_count"`
	MemoryUsage     int64         `json:"memory_usage_bytes"`
}

// DefaultPatternConfig returns a sensible default configuration
func DefaultPatternConfig() types.PatternConfig {
	return types.PatternConfig{
		EventWindow:        30 * time.Minute,
		CorrelationDelay:   5 * time.Second,
		MinConfidence:      0.85,
		MinEventsCount:     3,
		EnablePrediction:   true,
		EnableAutoFix:      false,
		MaxEventsPerWindow: 1000,
		BatchSize:          100,
	}
}

// PatternRegistry manages all available pattern detectors
type PatternRegistry struct {
	patterns map[string]types.PatternDetector
	mutex    sync.RWMutex

	// Performance tracking
	stats   map[string]*PatternStats
	statsMu sync.RWMutex
}

// PatternStats tracks performance metrics for a pattern detector
type PatternStats struct {
	TotalDetections int64 `json:"total_detections"`
	TruePositives   int64 `json:"true_positives"`
	FalsePositives  int64 `json:"false_positives"`
	TrueNegatives   int64 `json:"true_negatives"`
	FalseNegatives  int64 `json:"false_negatives"`

	TotalExecutions    int64         `json:"total_executions"`
	TotalExecutionTime time.Duration `json:"total_execution_time"`
	MaxExecutionTime   time.Duration `json:"max_execution_time"`

	LastExecution time.Time `json:"last_execution"`
	LastDetection time.Time `json:"last_detection"`
}

// Ensure PatternRegistry implements types.PatternRegistry
var _ types.PatternRegistry = (*PatternRegistry)(nil)

// NewPatternRegistry creates a new pattern registry
func NewPatternRegistry() *PatternRegistry {
	return &PatternRegistry{
		patterns: make(map[string]types.PatternDetector),
		stats:    make(map[string]*PatternStats),
	}
}

// Register adds a pattern detector to the registry
func (pr *PatternRegistry) Register(detector types.PatternDetector) error {
	pr.mutex.Lock()
	defer pr.mutex.Unlock()

	id := detector.ID()
	if _, exists := pr.patterns[id]; exists {
		return fmt.Errorf("pattern detector %s already registered", id)
	}

	pr.patterns[id] = detector

	pr.statsMu.Lock()
	pr.stats[id] = &PatternStats{}
	pr.statsMu.Unlock()

	return nil
}

// Unregister removes a pattern detector from the registry
func (pr *PatternRegistry) Unregister(patternID string) error {
	pr.mutex.Lock()
	defer pr.mutex.Unlock()

	if _, exists := pr.patterns[patternID]; !exists {
		return fmt.Errorf("pattern detector %s not found", patternID)
	}

	delete(pr.patterns, patternID)

	pr.statsMu.Lock()
	delete(pr.stats, patternID)
	pr.statsMu.Unlock()

	return nil
}

// Get retrieves a pattern detector by ID
func (pr *PatternRegistry) Get(id string) (types.PatternDetector, error) {
	pr.mutex.RLock()
	defer pr.mutex.RUnlock()

	detector, exists := pr.patterns[id]
	if !exists {
		return nil, fmt.Errorf("pattern detector %s not found", id)
	}
	return detector, nil
}

// List returns all registered pattern detectors
func (pr *PatternRegistry) List() []types.PatternDetector {
	pr.mutex.RLock()
	defer pr.mutex.RUnlock()

	detectors := make([]types.PatternDetector, 0, len(pr.patterns))
	for _, detector := range pr.patterns {
		detectors = append(detectors, detector)
	}

	return detectors
}

// ListByCategory returns pattern detectors for a specific category
func (pr *PatternRegistry) ListByCategory(category types.Category) []types.PatternDetector {
	pr.mutex.RLock()
	defer pr.mutex.RUnlock()

	var detectors []types.PatternDetector
	for _, detector := range pr.patterns {
		if detector.Category() == category {
			detectors = append(detectors, detector)
		}
	}

	return detectors
}

// DetectAll runs all pattern detectors and returns results
func (pr *PatternRegistry) DetectAll(ctx context.Context, events []types.Event, metrics map[string]types.MetricSeries) ([]types.PatternResult, error) {
	detectors := pr.List()
	results := make([]types.PatternResult, 0, len(detectors))

	// Run detectors concurrently for performance
	type detectionResult struct {
		result *types.PatternResult
		err    error
		id     string
	}

	resultChan := make(chan detectionResult, len(detectors))

	for _, detector := range detectors {
		go func(d types.PatternDetector) {
			start := time.Now()

			result, err := d.Detect(ctx, events, metrics)

			// Update stats
			pr.updateStats(d.ID(), time.Since(start), result != nil, err == nil)

			resultChan <- detectionResult{
				result: result,
				err:    err,
				id:     d.ID(),
			}
		}(detector)
	}

	// Collect results
	for i := 0; i < len(detectors); i++ {
		select {
		case res := <-resultChan:
			if res.err != nil {
				// Log error but continue with other detectors
				continue
			}
			if res.result != nil {
				results = append(results, *res.result)
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return results, nil
}

// updateStats updates performance statistics for a pattern detector
func (pr *PatternRegistry) updateStats(id string, duration time.Duration, detected, success bool) {
	pr.statsMu.Lock()
	defer pr.statsMu.Unlock()

	stats, exists := pr.stats[id]
	if !exists {
		stats = &PatternStats{}
		pr.stats[id] = stats
	}

	stats.TotalExecutions++
	stats.TotalExecutionTime += duration
	stats.LastExecution = time.Now()

	if duration > stats.MaxExecutionTime {
		stats.MaxExecutionTime = duration
	}

	if detected {
		stats.TotalDetections++
		stats.LastDetection = time.Now()
	}

	// Note: True/False positive tracking would require ground truth data
	// which would need to be provided externally through validation
}

// GetStats returns statistics for a pattern detector
func (pr *PatternRegistry) GetStats(id string) (*PatternStats, bool) {
	pr.statsMu.RLock()
	defer pr.statsMu.RUnlock()

	stats, exists := pr.stats[id]
	if !exists {
		return nil, false
	}

	// Return a copy to prevent mutation
	statsCopy := *stats
	return &statsCopy, true
}

// CalculateAccuracy calculates accuracy metrics for a pattern detector
func (stats *PatternStats) CalculateAccuracy() (accuracy, precision, recall, f1Score float64) {
	tp := float64(stats.TruePositives)
	fp := float64(stats.FalsePositives)
	tn := float64(stats.TrueNegatives)
	fn := float64(stats.FalseNegatives)

	total := tp + fp + tn + fn
	if total == 0 {
		return 0, 0, 0, 0
	}

	accuracy = (tp + tn) / total

	if tp+fp > 0 {
		precision = tp / (tp + fp)
	}

	if tp+fn > 0 {
		recall = tp / (tp + fn)
	}

	if precision+recall > 0 {
		f1Score = 2 * (precision * recall) / (precision + recall)
	}

	return accuracy, precision, recall, f1Score
}

// PatternValidator provides validation for pattern detection results
type PatternValidator struct {
	config types.PatternConfig
	mu     sync.RWMutex
}

// Ensure PatternValidator implements types.PatternValidator
var _ types.PatternValidator = (*PatternValidator)(nil)

// NewPatternValidator creates a new pattern validator
func NewPatternValidator(config types.PatternConfig) *PatternValidator {
	return &PatternValidator{
		config: config,
	}
}

// Validate validates a single pattern result
func (pv *PatternValidator) Validate(ctx context.Context, result *types.PatternResult) error {
	if result == nil {
		return fmt.Errorf("nil pattern result")
	}

	pv.mu.RLock()
	minConfidence := pv.config.MinConfidence
	pv.mu.RUnlock()

	if result.Confidence < minConfidence {
		return fmt.Errorf("confidence %.2f below minimum threshold %.2f", result.Confidence, minConfidence)
	}

	if len(result.MatchedEvents) < pv.config.MinEventsCount {
		return fmt.Errorf("matched events count %d below minimum %d", len(result.MatchedEvents), pv.config.MinEventsCount)
	}

	return nil
}

// ValidateBatch validates multiple pattern results
func (pv *PatternValidator) ValidateBatch(ctx context.Context, results []types.PatternResult) ([]types.PatternResult, []error) {
	valid := make([]types.PatternResult, 0, len(results))
	errors := make([]error, 0)

	for i, result := range results {
		if err := pv.Validate(ctx, &result); err != nil {
			errors = append(errors, fmt.Errorf("validation failed for result %d: %w", i, err))
			continue
		}
		valid = append(valid, result)
	}

	return valid, errors
}

// SetConfig updates the validator configuration
func (pv *PatternValidator) SetConfig(config types.PatternConfig) {
	pv.mu.Lock()
	defer pv.mu.Unlock()
	pv.config = config
}

// GetConfig returns the current validator configuration
func (pv *PatternValidator) GetConfig() types.PatternConfig {
	pv.mu.RLock()
	defer pv.mu.RUnlock()
	return pv.config
}

// StatisticalAnalyzer provides statistical analysis utilities for patterns
type StatisticalAnalyzer struct{}

// CalculateZScore calculates the z-score for anomaly detection
func (sa *StatisticalAnalyzer) CalculateZScore(value, mean, stddev float64) float64 {
	if stddev == 0 {
		return 0
	}
	return (value - mean) / stddev
}

// IsAnomaly determines if a value is anomalous based on z-score
func (sa *StatisticalAnalyzer) IsAnomaly(value, mean, stddev float64, threshold float64) bool {
	zScore := sa.CalculateZScore(value, mean, stddev)
	return math.Abs(zScore) > threshold
}

// CalculateMovingAverage calculates a simple moving average
func (sa *StatisticalAnalyzer) CalculateMovingAverage(values []float64, window int) []float64 {
	if len(values) < window {
		return nil
	}

	result := make([]float64, len(values)-window+1)

	for i := range result {
		sum := 0.0
		for j := i; j < i+window; j++ {
			sum += values[j]
		}
		result[i] = sum / float64(window)
	}

	return result
}

// CalculateExponentialMovingAverage calculates exponential moving average
func (sa *StatisticalAnalyzer) CalculateExponentialMovingAverage(values []float64, alpha float64) []float64 {
	if len(values) == 0 || alpha <= 0 || alpha > 1 {
		return nil
	}

	result := make([]float64, len(values))
	result[0] = values[0]

	for i := 1; i < len(values); i++ {
		result[i] = alpha*values[i] + (1-alpha)*result[i-1]
	}

	return result
}

// DetectTrend detects if there's a significant trend in the data
func (sa *StatisticalAnalyzer) DetectTrend(values []float64) (slope float64, confidence float64) {
	n := len(values)
	if n < 3 {
		return 0, 0
	}

	// Calculate linear regression slope
	sumX, sumY, sumXY, sumX2 := 0.0, 0.0, 0.0, 0.0

	for i, y := range values {
		x := float64(i)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	denominator := float64(n)*sumX2 - sumX*sumX
	if denominator == 0 {
		return 0, 0
	}

	slope = (float64(n)*sumXY - sumX*sumY) / denominator

	// Calculate R-squared for confidence
	yMean := sumY / float64(n)
	ssRes, ssTot := 0.0, 0.0

	for i, y := range values {
		x := float64(i)
		predicted := slope*x + (sumY-slope*sumX)/float64(n)
		ssRes += (y - predicted) * (y - predicted)
		ssTot += (y - yMean) * (y - yMean)
	}

	if ssTot == 0 {
		confidence = 1.0
	} else {
		confidence = 1.0 - ssRes/ssTot
	}

	return slope, confidence
}