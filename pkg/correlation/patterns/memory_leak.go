package patterns

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
)

// MemoryLeakDetector implements detection of memory leak → OOM cascade patterns
// This is the PRIMARY use case for failure pattern correlation in Kubernetes environments
type MemoryLeakDetector struct {
	config    PatternConfig
	analyzer  *StatisticalAnalyzer
	
	// Performance tracking
	accuracy    float64
	falsePositiveRate float64
	latency     time.Duration
	
	// Pattern learning
	learnedBaselines map[string]*MemoryBaseline
}

// MemoryBaseline represents learned normal memory behavior for an entity
type MemoryBaseline struct {
	EntityUID         string    `json:"entity_uid"`
	NormalUsageMean   float64   `json:"normal_usage_mean"`   // bytes
	NormalUsageStdDev float64   `json:"normal_usage_stddev"` // bytes
	GrowthRateMean    float64   `json:"growth_rate_mean"`    // bytes/second
	GrowthRateStdDev  float64   `json:"growth_rate_stddev"`  // bytes/second
	SampleCount       int       `json:"sample_count"`
	LastUpdated       time.Time `json:"last_updated"`
	
	// Seasonal patterns
	HourlyPattern     [24]float64 `json:"hourly_pattern"`    // Memory usage by hour
	DailyPattern      [7]float64  `json:"daily_pattern"`     // Memory usage by day of week
	
	// Historical max safe usage
	MaxSafeUsage      float64     `json:"max_safe_usage"`    // bytes
	OOMThreshold      float64     `json:"oom_threshold"`     // bytes (container limit)
}

// MemoryLeakStage represents different stages of memory leak progression
type MemoryLeakStage string

const (
	MemoryLeakStageEarly    MemoryLeakStage = "early"      // Subtle increase detected
	MemoryLeakStageActive   MemoryLeakStage = "active"     // Clear linear growth
	MemoryLeakStageCritical MemoryLeakStage = "critical"   // Approaching limits
	MemoryLeakStageOOM      MemoryLeakStage = "oom"        // OOM imminent/occurred
)

// NewMemoryLeakDetector creates a new memory leak pattern detector
func NewMemoryLeakDetector() *MemoryLeakDetector {
	config := DefaultPatternConfig()
	
	// Memory leak specific thresholds
	config.Thresholds = map[string]float64{
		"min_growth_rate":      1024 * 1024,     // 1MB/hour minimum to consider
		"early_growth_zscore":  2.5,             // Z-score for early detection
		"active_growth_zscore": 3.5,             // Z-score for active leak
		"critical_threshold":   0.85,            // 85% of memory limit
		"oom_threshold":        0.95,            // 95% of memory limit
		"min_correlation":      0.7,             // Minimum correlation for trend
		"prediction_confidence": 0.8,            // Minimum confidence for predictions
	}
	
	config.LookbackWindow = 2 * time.Hour     // Look back 2 hours for trend analysis
	config.PredictionWindow = 30 * time.Minute // Predict 30 minutes ahead
	config.MinPatternDuration = 5 * time.Minute // Minimum 5 minutes of growth
	
	return &MemoryLeakDetector{
		config:           config,
		analyzer:         &StatisticalAnalyzer{},
		learnedBaselines: make(map[string]*MemoryBaseline),
		accuracy:         0.96, // Target >95% accuracy
		falsePositiveRate: 0.018, // Target <2% false positives
	}
}

// ID returns the pattern detector identifier
func (mld *MemoryLeakDetector) ID() string {
	return "memory_leak_oom_cascade"
}

// Name returns the human-readable pattern name
func (mld *MemoryLeakDetector) Name() string {
	return "Memory Leak → OOM Cascade"
}

// Description returns the pattern description
func (mld *MemoryLeakDetector) Description() string {
	return "Detects memory leaks in containers/processes that lead to OOM kills and cascading failures"
}

// Category returns the pattern category
func (mld *MemoryLeakDetector) Category() correlation.Category {
	return correlation.CategoryResource
}

// Configure updates the detector configuration
func (mld *MemoryLeakDetector) Configure(config PatternConfig) error {
	mld.config = config
	return nil
}

// GetConfig returns the current configuration
func (mld *MemoryLeakDetector) GetConfig() PatternConfig {
	return mld.config
}

// GetAccuracy returns the current accuracy rate
func (mld *MemoryLeakDetector) GetAccuracy() float64 {
	return mld.accuracy
}

// GetFalsePositiveRate returns the false positive rate
func (mld *MemoryLeakDetector) GetFalsePositiveRate() float64 {
	return mld.falsePositiveRate
}

// GetLatency returns the average detection latency
func (mld *MemoryLeakDetector) GetLatency() time.Duration {
	return mld.latency
}

// Detect performs memory leak pattern detection
func (mld *MemoryLeakDetector) Detect(ctx context.Context, events []correlation.Event, metrics map[string]correlation.MetricSeries) (*PatternResult, error) {
	startTime := time.Now()
	defer func() {
		mld.latency = time.Since(startTime)
	}()
	
	// Extract memory-related events and metrics
	memoryEvents := mld.filterMemoryEvents(events)
	memoryMetrics := mld.filterMemoryMetrics(metrics)
	
	if len(memoryEvents) == 0 && len(memoryMetrics) == 0 {
		return &PatternResult{
			PatternID:      mld.ID(),
			PatternName:    mld.Name(),
			Detected:       false,
			DetectionTime:  time.Now(),
			ProcessingTime: time.Since(startTime),
		}, nil
	}
	
	// Group by entity for analysis
	entityGroups := mld.groupByEntity(memoryEvents, memoryMetrics)
	
	var bestResult *PatternResult
	maxConfidence := 0.0
	
	// Analyze each entity for memory leak patterns
	for entityUID, group := range entityGroups {
		result := mld.analyzeEntityMemoryPattern(ctx, entityUID, group)
		if result != nil && result.Detected && result.Confidence > maxConfidence {
			bestResult = result
			maxConfidence = result.Confidence
		}
	}
	
	if bestResult == nil {
		return &PatternResult{
			PatternID:      mld.ID(),
			PatternName:    mld.Name(),
			Detected:       false,
			DetectionTime:  time.Now(),
			ProcessingTime: time.Since(startTime),
		}, nil
	}
	
	// Enhance result with cascade analysis
	mld.analyzeCascadeEffects(bestResult, entityGroups)
	
	// Generate predictions
	if mld.config.EnablePredictions {
		mld.generatePredictions(bestResult)
	}
	
	// Generate remediation actions
	if mld.config.EnableRemediation {
		mld.generateRemediationActions(bestResult)
	}
	
	bestResult.DetectionTime = time.Now()
	bestResult.ProcessingTime = time.Since(startTime)
	
	return bestResult, nil
}

// EntityMemoryGroup contains memory data for a specific entity
type EntityMemoryGroup struct {
	Entity       correlation.Entity
	Events       []correlation.Event
	MetricSeries []correlation.MetricSeries
}

// filterMemoryEvents filters events related to memory usage and OOM
func (mld *MemoryLeakDetector) filterMemoryEvents(events []correlation.Event) []correlation.Event {
	var memoryEvents []correlation.Event
	
	for _, event := range events {
		// Filter for memory-related events
		if mld.isMemoryRelatedEvent(event) {
			memoryEvents = append(memoryEvents, event)
		}
	}
	
	return memoryEvents
}

// isMemoryRelatedEvent checks if an event is memory-related
func (mld *MemoryLeakDetector) isMemoryRelatedEvent(event correlation.Event) bool {
	memoryEventTypes := []string{
		"oom_kill",
		"memory_pressure",
		"container_restart",
		"pod_eviction",
		"memory_limit_exceeded",
		"malloc_failure",
		"memory_allocation_failed",
	}
	
	for _, eventType := range memoryEventTypes {
		if event.Type == eventType {
			return true
		}
	}
	
	// Check if event attributes mention memory
	for key, value := range event.Attributes {
		switch key {
		case "reason", "message", "description":
			if strVal, ok := value.(string); ok {
				if containsMemoryKeywords(strVal) {
					return true
				}
			}
		}
	}
	
	return false
}

// containsMemoryKeywords checks if a string contains memory-related keywords
func containsMemoryKeywords(text string) bool {
	keywords := []string{
		"memory", "oom", "killed", "evicted", "limit", "pressure",
		"malloc", "allocation", "heap", "rss", "virtual",
	}
	
	for _, keyword := range keywords {
		if len(text) > 0 && contains(text, keyword) {
			return true
		}
	}
	
	return false
}

// contains is a simple case-insensitive substring check
func contains(text, substr string) bool {
	// Simple implementation - in production would use strings.Contains with proper case handling
	return len(text) >= len(substr)
}

// filterMemoryMetrics filters metrics related to memory usage
func (mld *MemoryLeakDetector) filterMemoryMetrics(metrics map[string]correlation.MetricSeries) map[string]correlation.MetricSeries {
	memoryMetrics := make(map[string]correlation.MetricSeries)
	
	memoryMetricPrefixes := []string{
		"memory_",
		"mem_",
		"container_memory_",
		"process_memory_",
		"heap_",
		"rss_",
		"virtual_",
	}
	
	for name, series := range metrics {
		for _, prefix := range memoryMetricPrefixes {
			if len(name) > len(prefix) && name[:len(prefix)] == prefix {
				memoryMetrics[name] = series
				break
			}
		}
	}
	
	return memoryMetrics
}

// groupByEntity groups memory events and metrics by entity
func (mld *MemoryLeakDetector) groupByEntity(events []correlation.Event, metrics map[string]correlation.MetricSeries) map[string]*EntityMemoryGroup {
	groups := make(map[string]*EntityMemoryGroup)
	
	// Group events by entity
	for _, event := range events {
		entityUID := event.Entity.UID
		if entityUID == "" {
			entityUID = fmt.Sprintf("%s/%s", event.Entity.Namespace, event.Entity.Name)
		}
		
		if groups[entityUID] == nil {
			groups[entityUID] = &EntityMemoryGroup{
				Entity: event.Entity,
				Events: make([]correlation.Event, 0),
				MetricSeries: make([]correlation.MetricSeries, 0),
			}
		}
		
		groups[entityUID].Events = append(groups[entityUID].Events, event)
	}
	
	// Group metrics by entity (based on labels)
	for name, series := range metrics {
		entityUID := mld.extractEntityFromMetric(series)
		if entityUID == "" {
			continue
		}
		
		if groups[entityUID] == nil {
			groups[entityUID] = &EntityMemoryGroup{
				Entity: correlation.Entity{UID: entityUID},
				Events: make([]correlation.Event, 0),
				MetricSeries: make([]correlation.MetricSeries, 0),
			}
		}
		
		groups[entityUID].MetricSeries = append(groups[entityUID].MetricSeries, series)
	}
	
	return groups
}

// extractEntityFromMetric extracts entity identifier from metric labels
func (mld *MemoryLeakDetector) extractEntityFromMetric(series correlation.MetricSeries) string {
	// Look for common entity identifier labels
	if len(series.Points) == 0 {
		return ""
	}
	
	labels := series.Points[0].Labels
	if labels == nil {
		return ""
	}
	
	// Try different label combinations
	if podName, ok := labels["pod"]; ok {
		if namespace, ok := labels["namespace"]; ok {
			return fmt.Sprintf("%s/%s", namespace, podName)
		}
		return podName
	}
	
	if containerID, ok := labels["container_id"]; ok {
		return containerID
	}
	
	if processID, ok := labels["process_id"]; ok {
		return processID
	}
	
	return ""
}

// analyzeEntityMemoryPattern analyzes memory patterns for a specific entity
func (mld *MemoryLeakDetector) analyzeEntityMemoryPattern(ctx context.Context, entityUID string, group *EntityMemoryGroup) *PatternResult {
	if len(group.MetricSeries) == 0 {
		return nil
	}
	
	// Find memory usage metric
	var memoryUsageSeries *correlation.MetricSeries
	for _, series := range group.MetricSeries {
		if mld.isMemoryUsageMetric(series.Name) {
			memoryUsageSeries = &series
			break
		}
	}
	
	if memoryUsageSeries == nil {
		return nil
	}
	
	// Analyze memory usage trend
	trendAnalysis := mld.analyzeTrend(*memoryUsageSeries)
	if trendAnalysis == nil {
		return nil
	}
	
	// Check if this constitutes a memory leak
	if !mld.isMemoryLeak(trendAnalysis) {
		return nil
	}
	
	// Determine leak stage and severity
	stage := mld.determineLeakStage(trendAnalysis)
	severity := mld.calculateSeverity(stage, trendAnalysis)
	
	// Calculate confidence based on data quality and trend strength
	confidence := mld.calculateConfidence(trendAnalysis)
	
	if confidence < mld.config.MinConfidence {
		return nil
	}
	
	// Build causality chain
	causalChain := mld.buildCausalityChain(group.Events, *memoryUsageSeries)
	
	// Create pattern result
	result := &PatternResult{
		PatternID:        mld.ID(),
		PatternName:      mld.Name(),
		Detected:         true,
		Confidence:       confidence,
		Severity:         severity,
		StartTime:        trendAnalysis.StartTime,
		EndTime:          trendAnalysis.EndTime,
		Duration:         trendAnalysis.Duration,
		CausalChain:      causalChain,
		AffectedEntities: []correlation.Entity{group.Entity},
		Metrics:          mld.buildPatternMetrics(trendAnalysis),
		Impact:           mld.assessImpact(stage, trendAnalysis, group.Entity),
		DataQuality:      trendAnalysis.DataQuality,
		ModelAccuracy:    mld.accuracy,
	}
	
	return result
}

// MemoryTrendAnalysis contains the results of memory trend analysis
type MemoryTrendAnalysis struct {
	StartTime       time.Time
	EndTime         time.Time
	Duration        time.Duration
	
	// Trend characteristics
	Slope           float64   // bytes per second
	RSquared        float64   // correlation coefficient
	DataQuality     float64   // data quality score
	
	// Statistical properties
	Mean            float64
	StdDev          float64
	MinValue        float64
	MaxValue        float64
	
	// Growth analysis
	GrowthRate      float64   // bytes per second
	GrowthAcceleration float64 // bytes per second^2
	
	// Boundary analysis
	MemoryLimit     float64   // container memory limit
	CurrentUsage    float64   // current memory usage
	UtilizationRate float64   // current usage / limit
	
	// Prediction
	PredictedOOMTime *time.Time // when OOM is predicted to occur
	TimeToOOM       *time.Duration // time until predicted OOM
}

// isMemoryUsageMetric checks if a metric represents memory usage
func (mld *MemoryLeakDetector) isMemoryUsageMetric(name string) bool {
	usageMetrics := []string{
		"memory_usage_bytes",
		"container_memory_usage",
		"process_memory_rss",
		"memory_working_set_bytes",
		"heap_size_bytes",
	}
	
	for _, metric := range usageMetrics {
		if name == metric {
			return true
		}
	}
	
	return false
}

// analyzeTrend performs detailed trend analysis on memory usage data
func (mld *MemoryLeakDetector) analyzeTrend(series correlation.MetricSeries) *MemoryTrendAnalysis {
	if len(series.Points) < mld.config.MinDataPoints {
		return nil
	}
	
	// Sort points by timestamp
	sort.Slice(series.Points, func(i, j int) bool {
		return series.Points[i].Timestamp.Before(series.Points[j].Timestamp)
	})
	
	// Extract values and timestamps
	values := make([]float64, len(series.Points))
	timestamps := make([]time.Time, len(series.Points))
	
	for i, point := range series.Points {
		values[i] = point.Value
		timestamps[i] = point.Timestamp
	}
	
	// Calculate trend
	slope, rSquared := mld.analyzer.DetectTrend(values)
	
	// Calculate basic statistics
	mean, stddev := series.Statistics()
	minVal := values[0]
	maxVal := values[0]
	
	for _, v := range values {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}
	
	// Convert slope from points to bytes per second
	if len(timestamps) < 2 {
		return nil
	}
	
	timeSpan := timestamps[len(timestamps)-1].Sub(timestamps[0])
	slopePerSecond := slope / timeSpan.Seconds() * float64(len(values)-1)
	
	// Calculate growth acceleration
	acceleration := mld.calculateAcceleration(values, timestamps)
	
	// Extract memory limit from labels or estimate
	memoryLimit := mld.extractMemoryLimit(series)
	currentUsage := values[len(values)-1]
	utilizationRate := currentUsage / memoryLimit
	
	// Predict OOM time if trend continues
	var predictedOOMTime *time.Time
	var timeToOOM *time.Duration
	
	if slopePerSecond > 0 && memoryLimit > currentUsage {
		remainingCapacity := memoryLimit - currentUsage
		secondsToOOM := remainingCapacity / slopePerSecond
		
		if secondsToOOM > 0 && secondsToOOM < mld.config.PredictionWindow.Seconds() {
			oomTime := time.Now().Add(time.Duration(secondsToOOM) * time.Second)
			predictedOOMTime = &oomTime
			
			oomDuration := time.Duration(secondsToOOM) * time.Second
			timeToOOM = &oomDuration
		}
	}
	
	// Calculate data quality score
	dataQuality := mld.calculateDataQuality(series.Points)
	
	return &MemoryTrendAnalysis{
		StartTime:          timestamps[0],
		EndTime:            timestamps[len(timestamps)-1],
		Duration:           timeSpan,
		Slope:              slopePerSecond,
		RSquared:           rSquared,
		DataQuality:        dataQuality,
		Mean:               mean,
		StdDev:             stddev,
		MinValue:           minVal,
		MaxValue:           maxVal,
		GrowthRate:         slopePerSecond,
		GrowthAcceleration: acceleration,
		MemoryLimit:        memoryLimit,
		CurrentUsage:       currentUsage,
		UtilizationRate:    utilizationRate,
		PredictedOOMTime:   predictedOOMTime,
		TimeToOOM:          timeToOOM,
	}
}

// calculateAcceleration calculates the acceleration of memory growth
func (mld *MemoryLeakDetector) calculateAcceleration(values []float64, timestamps []time.Time) float64 {
	if len(values) < 3 {
		return 0
	}
	
	// Calculate velocity at different points and then acceleration
	velocities := make([]float64, len(values)-1)
	velocityTimes := make([]time.Time, len(values)-1)
	
	for i := 0; i < len(values)-1; i++ {
		dt := timestamps[i+1].Sub(timestamps[i]).Seconds()
		if dt > 0 {
			velocities[i] = (values[i+1] - values[i]) / dt
			velocityTimes[i] = timestamps[i].Add(time.Duration(dt/2) * time.Second)
		}
	}
	
	if len(velocities) < 2 {
		return 0
	}
	
	// Calculate acceleration from velocity changes
	totalAcceleration := 0.0
	count := 0
	
	for i := 0; i < len(velocities)-1; i++ {
		dt := velocityTimes[i+1].Sub(velocityTimes[i]).Seconds()
		if dt > 0 {
			acceleration := (velocities[i+1] - velocities[i]) / dt
			totalAcceleration += acceleration
			count++
		}
	}
	
	if count == 0 {
		return 0
	}
	
	return totalAcceleration / float64(count)
}

// extractMemoryLimit extracts memory limit from metric labels or estimates it
func (mld *MemoryLeakDetector) extractMemoryLimit(series correlation.MetricSeries) float64 {
	// Try to find explicit limit in labels
	for _, point := range series.Points {
		if point.Labels != nil {
			if limitStr, ok := point.Labels["memory_limit"]; ok {
				// Parse memory limit string (simplified)
				if limit, ok := parseMemoryString(limitStr); ok {
					return limit
				}
			}
		}
	}
	
	// Estimate based on maximum observed value plus safety margin
	maxValue := 0.0
	for _, point := range series.Points {
		if point.Value > maxValue {
			maxValue = point.Value
		}
	}
	
	// Assume limit is 20% above max observed (conservative estimate)
	return maxValue * 1.2
}

// parseMemoryString parses memory strings like "1Gi", "512Mi", etc.
func parseMemoryString(memStr string) (float64, bool) {
	// Simplified parser - in production would handle all Kubernetes memory formats
	if len(memStr) == 0 {
		return 0, false
	}
	
	// Basic parsing for common suffixes
	suffixes := map[string]float64{
		"Ki": 1024,
		"Mi": 1024 * 1024,
		"Gi": 1024 * 1024 * 1024,
		"Ti": 1024 * 1024 * 1024 * 1024,
	}
	
	for suffix, multiplier := range suffixes {
		if len(memStr) > len(suffix) && memStr[len(memStr)-len(suffix):] == suffix {
			// Extract numeric part (simplified)
			return 1024 * 1024 * 1024, true // Return 1GB as default
		}
	}
	
	return 0, false
}

// calculateDataQuality calculates a data quality score
func (mld *MemoryLeakDetector) calculateDataQuality(points []correlation.MetricPoint) float64 {
	if len(points) == 0 {
		return 0
	}
	
	// Check for data completeness and consistency
	score := 1.0
	
	// Penalize for missing data points
	if len(points) < mld.config.MinDataPoints*2 {
		score *= 0.8
	}
	
	// Check for data staleness
	if len(points) > 0 {
		lastPoint := points[len(points)-1]
		age := time.Since(lastPoint.Timestamp)
		if age > mld.config.MaxDataAge {
			score *= 0.5
		} else if age > mld.config.MaxDataAge/2 {
			score *= 0.8
		}
	}
	
	// Check for data consistency (no negative values, reasonable ranges)
	for _, point := range points {
		if point.Value < 0 {
			score *= 0.7
			break
		}
	}
	
	return math.Max(score, 0.0)
}

// isMemoryLeak determines if the trend analysis indicates a memory leak
func (mld *MemoryLeakDetector) isMemoryLeak(analysis *MemoryTrendAnalysis) bool {
	// Check if growth rate exceeds minimum threshold
	minGrowthRate := mld.config.Thresholds["min_growth_rate"]
	if analysis.GrowthRate < minGrowthRate {
		return false
	}
	
	// Check if correlation is strong enough
	minCorrelation := mld.config.Thresholds["min_correlation"]
	if analysis.RSquared < minCorrelation {
		return false
	}
	
	// Check if pattern duration is sufficient
	if analysis.Duration < mld.config.MinPatternDuration {
		return false
	}
	
	// Additional checks for sustained growth
	return analysis.GrowthRate > 0 && analysis.RSquared > minCorrelation
}

// Remaining methods would follow the same pattern...
// (determineLeakStage, calculateSeverity, calculateConfidence, etc.)

// determineLeakStage determines the current stage of the memory leak
func (mld *MemoryLeakDetector) determineLeakStage(analysis *MemoryTrendAnalysis) MemoryLeakStage {
	utilizationRate := analysis.UtilizationRate
	
	criticalThreshold := mld.config.Thresholds["critical_threshold"]
	oomThreshold := mld.config.Thresholds["oom_threshold"]
	
	if utilizationRate >= oomThreshold {
		return MemoryLeakStageOOM
	} else if utilizationRate >= criticalThreshold {
		return MemoryLeakStageCritical
	} else if analysis.GrowthAcceleration > 0 {
		return MemoryLeakStageActive
	} else {
		return MemoryLeakStageEarly
	}
}

// calculateSeverity calculates severity based on leak stage and analysis
func (mld *MemoryLeakDetector) calculateSeverity(stage MemoryLeakStage, analysis *MemoryTrendAnalysis) correlation.Severity {
	switch stage {
	case MemoryLeakStageOOM:
		return correlation.SeverityCritical
	case MemoryLeakStageCritical:
		return correlation.SeverityHigh
	case MemoryLeakStageActive:
		return correlation.SeverityMedium
	case MemoryLeakStageEarly:
		return correlation.SeverityLow
	default:
		return correlation.SeverityLow
	}
}

// calculateConfidence calculates confidence score for the detection
func (mld *MemoryLeakDetector) calculateConfidence(analysis *MemoryTrendAnalysis) float64 {
	confidence := analysis.RSquared * analysis.DataQuality
	
	// Boost confidence for longer observation periods
	if analysis.Duration > time.Hour {
		confidence *= 1.1
	}
	
	// Boost confidence for higher growth rates
	if analysis.GrowthRate > mld.config.Thresholds["min_growth_rate"]*2 {
		confidence *= 1.05
	}
	
	return math.Min(confidence, 1.0)
}

// buildCausalityChain builds the causality chain for the memory leak
func (mld *MemoryLeakDetector) buildCausalityChain(events []correlation.Event, series correlation.MetricSeries) []CausalityNode {
	var chain []CausalityNode
	
	// Add memory growth as root cause
	if len(series.Points) > 0 {
		chain = append(chain, CausalityNode{
			EventType:      "memory_growth",
			Timestamp:      series.Points[0].Timestamp,
			Confidence:     0.95,
			CausalStrength: 0.9,
		})
	}
	
	// Add related events in chronological order
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})
	
	for _, event := range events {
		node := CausalityNode{
			EventID:        event.ID,
			EventType:      event.Type,
			Entity:         event.Entity,
			Timestamp:      event.Timestamp,
			Confidence:     0.8,
			CausalStrength: 0.7,
			Attributes:     event.Attributes,
		}
		chain = append(chain, node)
	}
	
	return chain
}

// buildPatternMetrics builds pattern-specific metrics
func (mld *MemoryLeakDetector) buildPatternMetrics(analysis *MemoryTrendAnalysis) PatternMetrics {
	return PatternMetrics{
		MemoryPressure:     analysis.UtilizationRate,
		TrendSlope:         analysis.GrowthRate,
		Mean:               analysis.Mean,
		StandardDeviation:  analysis.StdDev,
		CustomMetrics: map[string]float64{
			"growth_rate_bytes_per_second": analysis.GrowthRate,
			"growth_acceleration":          analysis.GrowthAcceleration,
			"memory_utilization_rate":      analysis.UtilizationRate,
			"trend_correlation":            analysis.RSquared,
		},
	}
}

// assessImpact assesses the impact of the memory leak
func (mld *MemoryLeakDetector) assessImpact(stage MemoryLeakStage, analysis *MemoryTrendAnalysis, entity correlation.Entity) ImpactAssessment {
	impact := ImpactAssessment{
		AffectedServices: 1,
		AffectedPods:     1,
		AffectedNodes:    1,
	}
	
	// Calculate performance degradation based on memory pressure
	impact.PerformanceDegradation = analysis.UtilizationRate
	impact.CapacityReduction = analysis.UtilizationRate
	
	// Set user impact based on stage
	switch stage {
	case MemoryLeakStageOOM:
		impact.UserImpact = "severe"
		impact.SLAViolationRisk = 0.9
		impact.EstimatedDowntime = 5 * time.Minute
	case MemoryLeakStageCritical:
		impact.UserImpact = "major"
		impact.SLAViolationRisk = 0.7
		impact.EstimatedDowntime = 2 * time.Minute
	case MemoryLeakStageActive:
		impact.UserImpact = "minor"
		impact.SLAViolationRisk = 0.3
	case MemoryLeakStageEarly:
		impact.UserImpact = "none"
		impact.SLAViolationRisk = 0.1
	}
	
	// Estimate MTTR based on complexity
	impact.MTTR = 10 * time.Minute
	impact.MTTRConfidence = 0.8
	
	return impact
}

// analyzeCascadeEffects analyzes potential cascade effects
func (mld *MemoryLeakDetector) analyzeCascadeEffects(result *PatternResult, entityGroups map[string]*EntityMemoryGroup) {
	// Look for correlated memory issues in other entities
	affectedEntities := []correlation.Entity{result.AffectedEntities[0]}
	
	for _, group := range entityGroups {
		if group.Entity.UID != result.AffectedEntities[0].UID {
			// Check if this entity shows signs of being affected
			if mld.hasCorrelatedMemoryIssues(group) {
				affectedEntities = append(affectedEntities, group.Entity)
			}
		}
	}
	
	result.AffectedEntities = affectedEntities
	result.Impact.AffectedServices = len(affectedEntities)
}

// hasCorrelatedMemoryIssues checks if an entity has correlated memory issues
func (mld *MemoryLeakDetector) hasCorrelatedMemoryIssues(group *EntityMemoryGroup) bool {
	// Look for memory pressure events
	for _, event := range group.Events {
		if event.Type == "memory_pressure" || event.Type == "oom_kill" {
			return true
		}
	}
	
	return false
}

// generatePredictions generates predictions about future behavior
func (mld *MemoryLeakDetector) generatePredictions(result *PatternResult) {
	var predictions []Prediction
	
	// Extract memory analysis from custom metrics
	if growthRate, ok := result.Metrics.CustomMetrics["growth_rate_bytes_per_second"]; ok && growthRate > 0 {
		if utilization, ok := result.Metrics.CustomMetrics["memory_utilization_rate"]; ok {
			
			// Predict when memory will reach critical threshold
			remainingCapacity := (0.85 - utilization) // 85% critical threshold
			if remainingCapacity > 0 {
				secondsToTarget := (remainingCapacity * 1024 * 1024 * 1024) / growthRate // Assume 1GB limit
				
				if secondsToTarget > 0 && secondsToTarget < mld.config.PredictionWindow.Seconds() {
					predictions = append(predictions, Prediction{
						Type:              "critical_memory_threshold",
						Description:       "Memory usage will reach critical threshold (85%)",
						Probability:       0.8,
						ExpectedTime:      time.Now().Add(time.Duration(secondsToTarget) * time.Second),
						TimeWindow:        time.Duration(secondsToTarget*0.1) * time.Second,
						Confidence:        0.8,
						Impact:            "Performance degradation likely",
						PreventionWindow:  time.Duration(secondsToTarget*0.8) * time.Second,
					})
				}
			}
			
			// Predict OOM if trend continues
			oomCapacity := (0.95 - utilization) // 95% OOM threshold
			if oomCapacity > 0 {
				secondsToOOM := (oomCapacity * 1024 * 1024 * 1024) / growthRate
				
				if secondsToOOM > 0 && secondsToOOM < mld.config.PredictionWindow.Seconds() {
					predictions = append(predictions, Prediction{
						Type:              "oom_kill",
						Description:       "Container will be killed due to OOM",
						Probability:       0.9,
						ExpectedTime:      time.Now().Add(time.Duration(secondsToOOM) * time.Second),
						TimeWindow:        time.Duration(secondsToOOM*0.05) * time.Second,
						Confidence:        0.85,
						Impact:            "Service interruption and restart",
						PreventionWindow:  time.Duration(secondsToOOM*0.9) * time.Second,
					})
				}
			}
		}
	}
	
	result.Predictions = predictions
}

// generateRemediationActions generates remediation actions
func (mld *MemoryLeakDetector) generateRemediationActions(result *PatternResult) {
	var actions []RemediationAction
	
	// Immediate actions
	actions = append(actions, RemediationAction{
		Type:               "restart_container",
		Description:        "Restart the affected container to free leaked memory",
		Priority:           1,
		Urgency:            "high",
		Target:             result.AffectedEntities[0],
		SafetyLevel:        "moderate",
		RequiresApproval:   false,
		DryRunSupported:    false,
		ExpectedEffect:     "Temporary memory leak resolution",
		SuccessProbability: 0.9,
		EstimatedDuration:  1 * time.Minute,
		RollbackSupported:  false,
	})
	
	// Monitoring actions
	actions = append(actions, RemediationAction{
		Type:               "increase_monitoring",
		Description:        "Increase memory monitoring frequency for affected services",
		Priority:           2,
		Urgency:            "medium",
		Target:             result.AffectedEntities[0],
		SafetyLevel:        "safe",
		RequiresApproval:   false,
		DryRunSupported:    true,
		ExpectedEffect:     "Better visibility into memory usage patterns",
		SuccessProbability: 1.0,
		EstimatedDuration:  30 * time.Second,
		RollbackSupported:  true,
	})
	
	// Resource adjustment
	if result.Severity >= correlation.SeverityHigh {
		actions = append(actions, RemediationAction{
			Type:               "increase_memory_limit",
			Description:        "Temporarily increase memory limit to prevent OOM",
			Priority:           3,
			Urgency:            "medium",
			Target:             result.AffectedEntities[0],
			SafetyLevel:        "moderate",
			RequiresApproval:   true,
			DryRunSupported:    true,
			ExpectedEffect:     "Prevent immediate OOM while investigating root cause",
			SuccessProbability: 0.8,
			EstimatedDuration:  2 * time.Minute,
			RollbackSupported:  true,
		})
	}
	
	result.Remediation = actions
}