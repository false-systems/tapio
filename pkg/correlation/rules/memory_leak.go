package rules

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
)

// MemoryLeakRule detects memory leaks using pattern analysis
type MemoryLeakRule struct {
	*correlation.BaseRule
	config MemoryLeakConfig
}

// MemoryLeakConfig configures the memory leak detection rule
type MemoryLeakConfig struct {
	MinObservationPeriod time.Duration `json:"min_observation_period"` // Minimum time to observe before declaring a leak
	MinDataPoints        int           `json:"min_data_points"`        // Minimum data points needed for analysis
	LeakThreshold        float64       `json:"leak_threshold"`         // Minimum growth rate to consider a leak (bytes/minute)
	ConsistencyThreshold float64       `json:"consistency_threshold"`  // Required consistency in growth pattern
	MinConfidence        float64       `json:"min_confidence"`         // Minimum confidence to report findings
	MaxResetTolerance    int           `json:"max_reset_tolerance"`    // Maximum number of memory resets to tolerate
	GrowthAcceleration   float64       `json:"growth_acceleration"`    // Factor that indicates accelerating growth
	UseEBPFData          bool          `json:"use_ebpf_data"`
	UseMetricsData       bool          `json:"use_metrics_data"`
	UseKubernetesData    bool          `json:"use_kubernetes_data"`
}

// DefaultMemoryLeakConfig returns default configuration for memory leak detection
func DefaultMemoryLeakConfig() MemoryLeakConfig {
	return MemoryLeakConfig{
		MinObservationPeriod: 10 * time.Minute,
		MinDataPoints:        10,
		LeakThreshold:        1024 * 1024, // 1MB per minute
		ConsistencyThreshold: 0.7,         // 70% consistency
		MinConfidence:        0.8,         // 80% confidence
		MaxResetTolerance:    2,           // Allow 2 resets
		GrowthAcceleration:   1.5,         // 50% acceleration
		UseEBPFData:          true,
		UseMetricsData:       true,
		UseKubernetesData:    true,
	}
}

// NewMemoryLeakRule creates a new memory leak detection rule
func NewMemoryLeakRule(config MemoryLeakConfig) *MemoryLeakRule {
	metadata := correlation.RuleMetadata{
		ID:          "memory_leak_detection",
		Name:        "Memory Leak Detection",
		Description: "Detects memory leaks by analyzing sustained memory growth patterns",
		Version:     "1.0.0",
		Author:      "Tapio Correlation Engine",
		Tags:        []string{"memory", "leak", "performance", "stability"},
		Requirements: []correlation.RuleRequirement{
			{
				SourceType: correlation.SourceKubernetes,
				DataType:   "full",
				Required:   true,
				Fallback:   "basic_pod_info",
			},
			{
				SourceType: correlation.SourceEBPF,
				DataType:   "memory_stats",
				Required:   false,
				Fallback:   "metrics_data",
			},
			{
				SourceType: correlation.SourceMetrics,
				DataType:   "current",
				Required:   false,
				Fallback:   "kubernetes_metrics",
			},
		},
		Enabled:   true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	return &MemoryLeakRule{
		BaseRule: correlation.NewBaseRule(metadata),
		config:   config,
	}
}

// CheckRequirements verifies that required data sources are available
func (r *MemoryLeakRule) CheckRequirements(ctx context.Context, data *correlation.DataCollection) error {
	// Kubernetes data is required
	if !data.IsSourceAvailable(correlation.SourceKubernetes) {
		return correlation.NewRequirementNotMetError(r.GetMetadata().ID, r.GetMetadata().Requirements[0])
	}

	// At least one additional data source is needed for accurate detection
	hasAdditionalSource := false
	if r.config.UseEBPFData && data.IsSourceAvailable(correlation.SourceEBPF) {
		hasAdditionalSource = true
	}
	if r.config.UseMetricsData && data.IsSourceAvailable(correlation.SourceMetrics) {
		hasAdditionalSource = true
	}

	if !hasAdditionalSource {
		return fmt.Errorf("memory leak detection requires at least one additional data source (eBPF or metrics)")
	}

	return nil
}

// Execute runs the memory leak detection rule
func (r *MemoryLeakRule) Execute(ctx context.Context, ruleCtx *correlation.RuleContext) ([]correlation.Finding, error) {
	var findings []correlation.Finding

	// Get Kubernetes data
	k8sData, err := ruleCtx.DataCollection.GetKubernetesData(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes data: %w", err)
	}

	// Get additional data sources
	var ebpfData *correlation.EBPFData
	var metricsData *correlation.MetricsData

	if r.config.UseEBPFData && ruleCtx.DataCollection.IsSourceAvailable(correlation.SourceEBPF) {
		ebpfData, err = ruleCtx.DataCollection.GetEBPFData(ctx)
		if err != nil {
			ebpfData = nil
		}
	}

	if r.config.UseMetricsData && ruleCtx.DataCollection.IsSourceAvailable(correlation.SourceMetrics) {
		metricsData, err = ruleCtx.DataCollection.GetMetricsData(ctx)
		if err != nil {
			metricsData = nil
		}
	}

	// Analyze each pod for memory leaks
	for _, pod := range k8sData.Pods {
		if pod.Status.Phase != "Running" {
			continue
		}

		podFindings := r.analyzePodMemoryLeak(pod, k8sData, ebpfData, metricsData)
		findings = append(findings, podFindings...)
	}

	return findings, nil
}

// analyzePodMemoryLeak analyzes a specific pod for memory leaks
func (r *MemoryLeakRule) analyzePodMemoryLeak(pod interface{}, k8sData *correlation.KubernetesData, ebpfData *correlation.EBPFData, metricsData *correlation.MetricsData) []correlation.Finding {
	var findings []correlation.Finding

	// Extract pod information (simplified for this example)
	podName := fmt.Sprintf("pod-%d", time.Now().UnixNano())
	podNamespace := "default"

	// Analyze memory patterns from different sources
	leakAnalysis := r.analyzeMemoryLeakPatterns(podName, podNamespace, k8sData, ebpfData, metricsData)

	// Generate findings based on analysis
	if leakAnalysis.ConfidenceLevel >= r.config.MinConfidence {
		finding := r.createMemoryLeakFinding(podName, podNamespace, leakAnalysis)
		findings = append(findings, *finding)
	}

	return findings
}

// MemoryLeakAnalysis contains the results of memory leak analysis
type MemoryLeakAnalysis struct {
	LeakDetected       bool          `json:"leak_detected"`
	ConfidenceLevel    float64       `json:"confidence_level"`
	GrowthRate         float64       `json:"growth_rate"`        // bytes per minute
	GrowthConsistency  float64       `json:"growth_consistency"` // 0-1, higher is more consistent
	ObservationPeriod  time.Duration `json:"observation_period"`
	DataPoints         int           `json:"data_points"`
	MemoryResets       int           `json:"memory_resets"`
	AcceleratingGrowth bool          `json:"accelerating_growth"`
	PeakMemoryUsage    float64       `json:"peak_memory_usage"`
	AverageGrowthRate  float64       `json:"average_growth_rate"`
	DataSources        []string      `json:"data_sources"`
	LeakIndicators     []string      `json:"leak_indicators"`
}

// analyzeMemoryLeakPatterns analyzes memory patterns from all available sources
func (r *MemoryLeakRule) analyzeMemoryLeakPatterns(podName, podNamespace string, k8sData *correlation.KubernetesData, ebpfData *correlation.EBPFData, metricsData *correlation.MetricsData) MemoryLeakAnalysis {
	analysis := MemoryLeakAnalysis{
		DataSources:    make([]string, 0),
		LeakIndicators: make([]string, 0),
	}

	var allDataPoints []correlation.MemoryDataPoint

	// Analyze eBPF data if available
	if ebpfData != nil {
		ebpfPoints := r.analyzeEBPFMemoryPatterns(podName, ebpfData, &analysis)
		allDataPoints = append(allDataPoints, ebpfPoints...)
	}

	// Analyze metrics data if available
	if metricsData != nil {
		metricsPoints := r.analyzeMetricsMemoryPatterns(podName, podNamespace, metricsData, &analysis)
		allDataPoints = append(allDataPoints, metricsPoints...)
	}

	// Analyze Kubernetes data
	k8sPoints := r.analyzeKubernetesMemoryPatterns(podName, podNamespace, k8sData, &analysis)
	allDataPoints = append(allDataPoints, k8sPoints...)

	// Perform comprehensive leak analysis
	if len(allDataPoints) >= r.config.MinDataPoints {
		r.performLeakAnalysis(allDataPoints, &analysis)
	}

	return analysis
}

// analyzeEBPFMemoryPatterns analyzes eBPF memory data for leak patterns
func (r *MemoryLeakRule) analyzeEBPFMemoryPatterns(podName string, ebpfData *correlation.EBPFData, analysis *MemoryLeakAnalysis) []correlation.MemoryDataPoint {
	analysis.DataSources = append(analysis.DataSources, "eBPF")

	var allPoints []correlation.MemoryDataPoint

	// Collect data points from all relevant processes
	for _, processStats := range ebpfData.ProcessStats {
		if processStats.InContainer {
			allPoints = append(allPoints, processStats.GrowthPattern...)

			// Check for allocation patterns that indicate leaks
			if processStats.AllocationRate > r.config.LeakThreshold {
				analysis.LeakIndicators = append(analysis.LeakIndicators, "high_allocation_rate")
			}

			// Check for low free rate compared to allocation rate
			if processStats.TotalAllocated > 0 && processStats.TotalFreed > 0 {
				freeRate := float64(processStats.TotalFreed) / float64(processStats.TotalAllocated)
				if freeRate < 0.9 { // Less than 90% of allocated memory is freed
					analysis.LeakIndicators = append(analysis.LeakIndicators, "low_free_rate")
				}
			}
		}
	}

	return allPoints
}

// analyzeMetricsMemoryPatterns analyzes metrics data for leak patterns
func (r *MemoryLeakRule) analyzeMetricsMemoryPatterns(podName, podNamespace string, metricsData *correlation.MetricsData, analysis *MemoryLeakAnalysis) []correlation.MemoryDataPoint {
	analysis.DataSources = append(analysis.DataSources, "metrics")

	var points []correlation.MemoryDataPoint

	// Find pod metrics
	for _, podMetrics := range metricsData.PodMetrics {
		if podMetrics.Name == podName && podMetrics.Namespace == podNamespace {
			// Create data point from current metrics
			point := correlation.MemoryDataPoint{
				Timestamp: metricsData.Timestamp,
				Usage:     uint64(podMetrics.Memory.Current),
			}
			points = append(points, point)

			// Check for sustained growth trend
			if podMetrics.Memory.Trend > 0 {
				analysis.LeakIndicators = append(analysis.LeakIndicators, "sustained_growth_trend")
			}

			// Check for high memory usage
			if podMetrics.Memory.Usage > 0.8 {
				analysis.LeakIndicators = append(analysis.LeakIndicators, "high_memory_usage")
			}

			break
		}
	}

	return points
}

// analyzeKubernetesMemoryPatterns analyzes Kubernetes data for leak patterns
func (r *MemoryLeakRule) analyzeKubernetesMemoryPatterns(podName, podNamespace string, k8sData *correlation.KubernetesData, analysis *MemoryLeakAnalysis) []correlation.MemoryDataPoint {
	analysis.DataSources = append(analysis.DataSources, "kubernetes")

	var points []correlation.MemoryDataPoint

	// Check for OOMKilled events in recent history
	for _, event := range k8sData.Events {
		if event.Reason == "OOMKilled" && event.InvolvedObject.Name == podName {
			analysis.LeakIndicators = append(analysis.LeakIndicators, "oom_killed_events")
		}
	}

	// Check for frequent restarts which might indicate memory issues
	for range k8sData.Pods {
		// Simplified: in real implementation, you'd parse the actual pod object
		// and check container restart counts
		analysis.LeakIndicators = append(analysis.LeakIndicators, "kubernetes_analysis")
		break // Just add the indicator once if pods exist
	}

	return points
}

// performLeakAnalysis performs comprehensive leak analysis on collected data points
func (r *MemoryLeakRule) performLeakAnalysis(dataPoints []correlation.MemoryDataPoint, analysis *MemoryLeakAnalysis) {
	// Sort data points by timestamp
	sort.Slice(dataPoints, func(i, j int) bool {
		return dataPoints[i].Timestamp.Before(dataPoints[j].Timestamp)
	})

	analysis.DataPoints = len(dataPoints)
	analysis.ObservationPeriod = dataPoints[len(dataPoints)-1].Timestamp.Sub(dataPoints[0].Timestamp)

	// Calculate growth statistics
	growthRates := r.calculateGrowthRates(dataPoints)
	analysis.GrowthRate = r.calculateAverageGrowthRate(growthRates)
	analysis.GrowthConsistency = r.calculateGrowthConsistency(growthRates)

	// Detect memory resets (garbage collection, restarts, etc.)
	analysis.MemoryResets = r.detectMemoryResets(dataPoints)

	// Check for accelerating growth
	analysis.AcceleratingGrowth = r.detectAcceleratingGrowth(growthRates)

	// Find peak memory usage
	analysis.PeakMemoryUsage = r.findPeakMemoryUsage(dataPoints)

	// Calculate overall confidence
	analysis.ConfidenceLevel = r.calculateLeakConfidence(analysis)

	// Determine if leak is detected
	analysis.LeakDetected = analysis.ConfidenceLevel >= r.config.MinConfidence &&
		analysis.GrowthRate >= r.config.LeakThreshold &&
		analysis.GrowthConsistency >= r.config.ConsistencyThreshold &&
		analysis.ObservationPeriod >= r.config.MinObservationPeriod
}

// calculateGrowthRates calculates growth rates between consecutive data points
func (r *MemoryLeakRule) calculateGrowthRates(dataPoints []correlation.MemoryDataPoint) []float64 {
	var growthRates []float64

	for i := 1; i < len(dataPoints); i++ {
		timeDiff := dataPoints[i].Timestamp.Sub(dataPoints[i-1].Timestamp)
		if timeDiff > 0 {
			usageDiff := int64(dataPoints[i].Usage) - int64(dataPoints[i-1].Usage)
			rate := float64(usageDiff) / timeDiff.Minutes()
			growthRates = append(growthRates, rate)
		}
	}

	return growthRates
}

// calculateAverageGrowthRate calculates the average growth rate
func (r *MemoryLeakRule) calculateAverageGrowthRate(growthRates []float64) float64 {
	if len(growthRates) == 0 {
		return 0
	}

	var sum float64
	for _, rate := range growthRates {
		sum += rate
	}

	return sum / float64(len(growthRates))
}

// calculateGrowthConsistency calculates how consistent the growth pattern is
func (r *MemoryLeakRule) calculateGrowthConsistency(growthRates []float64) float64 {
	if len(growthRates) < 2 {
		return 0
	}

	// Calculate standard deviation
	mean := r.calculateAverageGrowthRate(growthRates)
	var variance float64

	for _, rate := range growthRates {
		variance += math.Pow(rate-mean, 2)
	}

	variance /= float64(len(growthRates))
	stdDev := math.Sqrt(variance)

	// Consistency is inversely related to standard deviation
	// If mean is 0, we can't calculate consistency
	if mean == 0 {
		return 0
	}

	// Coefficient of variation (CV) = stdDev / |mean|
	cv := stdDev / math.Abs(mean)

	// Convert to consistency score (0-1, higher is more consistent)
	consistency := 1.0 / (1.0 + cv)

	return consistency
}

// detectMemoryResets detects significant drops in memory usage
func (r *MemoryLeakRule) detectMemoryResets(dataPoints []correlation.MemoryDataPoint) int {
	resets := 0

	for i := 1; i < len(dataPoints); i++ {
		currentUsage := dataPoints[i].Usage
		prevUsage := dataPoints[i-1].Usage

		// A reset is detected if memory usage drops by more than 50%
		if prevUsage > 0 && currentUsage < prevUsage/2 {
			resets++
		}
	}

	return resets
}

// detectAcceleratingGrowth detects if memory growth is accelerating
func (r *MemoryLeakRule) detectAcceleratingGrowth(growthRates []float64) bool {
	if len(growthRates) < 4 {
		return false
	}

	// Split growth rates into two halves and compare averages
	mid := len(growthRates) / 2
	firstHalf := growthRates[:mid]
	secondHalf := growthRates[mid:]

	firstAvg := r.calculateAverageGrowthRate(firstHalf)
	secondAvg := r.calculateAverageGrowthRate(secondHalf)

	// Growth is accelerating if the second half has significantly higher growth
	return secondAvg > firstAvg*r.config.GrowthAcceleration
}

// findPeakMemoryUsage finds the peak memory usage in the data points
func (r *MemoryLeakRule) findPeakMemoryUsage(dataPoints []correlation.MemoryDataPoint) float64 {
	var peak uint64

	for _, point := range dataPoints {
		if point.Usage > peak {
			peak = point.Usage
		}
	}

	return float64(peak)
}

// calculateLeakConfidence calculates the overall confidence level for leak detection
func (r *MemoryLeakRule) calculateLeakConfidence(analysis *MemoryLeakAnalysis) float64 {
	var confidenceFactors []float64

	// Growth rate factor
	if analysis.GrowthRate >= r.config.LeakThreshold {
		rateFactor := math.Min(analysis.GrowthRate/r.config.LeakThreshold, 2.0) / 2.0
		confidenceFactors = append(confidenceFactors, rateFactor)
	}

	// Consistency factor
	confidenceFactors = append(confidenceFactors, analysis.GrowthConsistency)

	// Observation period factor
	if analysis.ObservationPeriod >= r.config.MinObservationPeriod {
		periodFactor := math.Min(float64(analysis.ObservationPeriod)/float64(r.config.MinObservationPeriod), 2.0) / 2.0
		confidenceFactors = append(confidenceFactors, periodFactor)
	}

	// Data points factor
	if analysis.DataPoints >= r.config.MinDataPoints {
		pointsFactor := math.Min(float64(analysis.DataPoints)/float64(r.config.MinDataPoints), 2.0) / 2.0
		confidenceFactors = append(confidenceFactors, pointsFactor)
	}

	// Memory resets penalty
	resetPenalty := 1.0
	if analysis.MemoryResets > r.config.MaxResetTolerance {
		resetPenalty = 1.0 / (1.0 + float64(analysis.MemoryResets-r.config.MaxResetTolerance)*0.2)
	}

	// Accelerating growth bonus
	accelerationBonus := 1.0
	if analysis.AcceleratingGrowth {
		accelerationBonus = 1.2
	}

	// Data source factor
	dataSourceFactor := 1.0
	if len(analysis.DataSources) > 1 {
		dataSourceFactor = 1.0 + float64(len(analysis.DataSources)-1)*0.1
	}

	// Leak indicators factor
	indicatorFactor := 1.0
	if len(analysis.LeakIndicators) > 0 {
		indicatorFactor = 1.0 + float64(len(analysis.LeakIndicators))*0.05
	}

	// Calculate weighted average
	if len(confidenceFactors) == 0 {
		return 0
	}

	var totalConfidence float64
	for _, factor := range confidenceFactors {
		totalConfidence += factor
	}

	averageConfidence := totalConfidence / float64(len(confidenceFactors))
	finalConfidence := averageConfidence * resetPenalty * accelerationBonus * dataSourceFactor * indicatorFactor

	return math.Min(finalConfidence, 1.0)
}

// createMemoryLeakFinding creates a finding for memory leak detection
func (r *MemoryLeakRule) createMemoryLeakFinding(podName, podNamespace string, analysis MemoryLeakAnalysis) *correlation.Finding {
	// Determine severity based on confidence and growth rate
	var severity correlation.Severity
	if analysis.ConfidenceLevel >= 0.9 && analysis.GrowthRate >= r.config.LeakThreshold*2 {
		severity = correlation.SeverityCritical
	} else if analysis.ConfidenceLevel >= 0.8 {
		severity = correlation.SeverityError
	} else {
		severity = correlation.SeverityWarning
	}

	// Create finding
	finding := r.CreateFinding(
		"Memory Leak Detected",
		fmt.Sprintf("Pod %s/%s shows sustained memory growth patterns indicating a memory leak", podNamespace, podName),
		severity,
		analysis.ConfidenceLevel,
	)

	// Add resource reference
	finding.Resource = correlation.ResourceInfo{
		Type:      "Pod",
		Name:      podName,
		Namespace: podNamespace,
	}

	// Add prediction for memory exhaustion
	if analysis.GrowthRate > 0 {
		// Estimate time to memory exhaustion (simplified)
		estimatedTimeToExhaustion := 24 * time.Hour // Default estimate
		finding.Prediction = &correlation.Prediction{
			Event:       "Memory Exhaustion",
			TimeToEvent: estimatedTimeToExhaustion,
			Confidence:  analysis.ConfidenceLevel,
			Factors:     analysis.LeakIndicators,
			Mitigation: []string{
				"Analyze application for memory leaks",
				"Implement proper memory management",
				"Add memory profiling to identify leak sources",
				"Consider implementing garbage collection tuning",
				"Add memory monitoring and alerting",
			},
			UpdatedAt: time.Now(),
		}
	}

	// Add evidence from different sources
	for _, source := range analysis.DataSources {
		evidence := correlation.Evidence{
			Type:        "memory_leak_pattern",
			Source:      correlation.SourceType(source),
			Description: fmt.Sprintf("Memory growth pattern analysis from %s", source),
			Data: map[string]interface{}{
				"growth_rate":         analysis.GrowthRate,
				"growth_consistency":  analysis.GrowthConsistency,
				"observation_period":  analysis.ObservationPeriod,
				"data_points":         analysis.DataPoints,
				"memory_resets":       analysis.MemoryResets,
				"accelerating_growth": analysis.AcceleratingGrowth,
				"peak_memory_usage":   analysis.PeakMemoryUsage,
			},
			Timestamp:  time.Now(),
			Confidence: analysis.ConfidenceLevel,
		}
		finding.AddEvidence(evidence)
	}

	// Add tags
	finding.AddTag("memory")
	finding.AddTag("leak")
	finding.AddTag("performance")
	finding.AddTag("stability")

	// Add metadata
	finding.SetMetadata("analysis", analysis)
	finding.SetMetadata("leak_indicators", analysis.LeakIndicators)
	finding.SetMetadata("data_sources", analysis.DataSources)

	return finding
}

// GetConfidenceFactors returns factors that affect confidence scoring
func (r *MemoryLeakRule) GetConfidenceFactors() []string {
	return []string{
		"growth_rate_consistency",
		"observation_period_length",
		"data_points_quantity",
		"multiple_data_sources",
		"leak_pattern_indicators",
		"memory_reset_frequency",
		"growth_acceleration",
	}
}

// Validate validates the rule configuration
func (r *MemoryLeakRule) Validate() error {
	if err := r.BaseRule.Validate(); err != nil {
		return err
	}

	if r.config.MinObservationPeriod <= 0 {
		return correlation.NewRuleValidationError("min_observation_period must be positive")
	}

	if r.config.MinDataPoints < 3 {
		return correlation.NewRuleValidationError("min_data_points must be at least 3")
	}

	if r.config.LeakThreshold <= 0 {
		return correlation.NewRuleValidationError("leak_threshold must be positive")
	}

	if r.config.ConsistencyThreshold <= 0 || r.config.ConsistencyThreshold > 1 {
		return correlation.NewRuleValidationError("consistency_threshold must be between 0 and 1")
	}

	if r.config.MinConfidence <= 0 || r.config.MinConfidence > 1 {
		return correlation.NewRuleValidationError("min_confidence must be between 0 and 1")
	}

	if r.config.GrowthAcceleration <= 1 {
		return correlation.NewRuleValidationError("growth_acceleration must be greater than 1")
	}

	return nil
}
