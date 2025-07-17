package rules

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation"
)

// CPUThrottlingRule detects CPU throttling affecting performance
type CPUThrottlingRule struct {
	*correlation.BaseRule
	config CPUThrottlingConfig
}

// CPUThrottlingConfig configures the CPU throttling detection rule
type CPUThrottlingConfig struct {
	MinObservationPeriod time.Duration `json:"min_observation_period"`
	MinDataPoints        int           `json:"min_data_points"`
	ThrottlingThreshold  float64       `json:"throttling_threshold"` // Percentage of time throttled
	ImpactThreshold      float64       `json:"impact_threshold"`     // Performance impact threshold
	MinConfidence        float64       `json:"min_confidence"`
	UseEBPFData          bool          `json:"use_ebpf_data"`
	UseMetricsData       bool          `json:"use_metrics_data"`
	UseKubernetesData    bool          `json:"use_kubernetes_data"`
}

// DefaultCPUThrottlingConfig returns default configuration
func DefaultCPUThrottlingConfig() CPUThrottlingConfig {
	return CPUThrottlingConfig{
		MinObservationPeriod: 5 * time.Minute,
		MinDataPoints:        5,
		ThrottlingThreshold:  0.2, // 20% throttled time
		ImpactThreshold:      0.1, // 10% performance impact
		MinConfidence:        0.7,
		UseEBPFData:          true,
		UseMetricsData:       true,
		UseKubernetesData:    true,
	}
}

// NewCPUThrottlingRule creates a new CPU throttling detection rule
func NewCPUThrottlingRule(config CPUThrottlingConfig) *CPUThrottlingRule {
	metadata := correlation.RuleMetadata{
		ID:          "cpu_throttling",
		Name:        "CPU Throttling Detection",
		Description: "Detects CPU throttling that impacts application performance",
		Version:     "1.0.0",
		Author:      "Tapio Correlation Engine",
		Tags:        []string{"cpu", "performance", "throttling", "resource"},
		Requirements: []correlation.RuleRequirement{
			{
				SourceType: correlation.SourceKubernetes,
				DataType:   "full",
				Required:   true,
				Fallback:   "basic_pod_info",
			},
			{
				SourceType: correlation.SourceMetrics,
				DataType:   "current",
				Required:   false,
				Fallback:   "kubernetes_metrics",
			},
			{
				SourceType: correlation.SourceEBPF,
				DataType:   "cpu_stats",
				Required:   false,
				Fallback:   "metrics_data",
			},
		},
		Enabled:   true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	return &CPUThrottlingRule{
		BaseRule: correlation.NewBaseRule(metadata),
		config:   config,
	}
}

// CheckRequirements verifies that required data sources are available
func (r *CPUThrottlingRule) CheckRequirements(ctx context.Context, data *correlation.DataCollection) error {
	// Kubernetes data is required
	if !data.IsSourceAvailable(correlation.SourceKubernetes) {
		return correlation.NewRequirementNotMetError(r.GetMetadata().ID, r.GetMetadata().Requirements[0])
	}

	// At least one additional data source is needed
	hasAdditionalSource := false
	if r.config.UseMetricsData && data.IsSourceAvailable(correlation.SourceMetrics) {
		hasAdditionalSource = true
	}
	if r.config.UseEBPFData && data.IsSourceAvailable(correlation.SourceEBPF) {
		hasAdditionalSource = true
	}

	if !hasAdditionalSource {
		return fmt.Errorf("CPU throttling detection requires at least one additional data source (metrics or eBPF)")
	}

	return nil
}

// Execute runs the CPU throttling detection rule
func (r *CPUThrottlingRule) Execute(ctx context.Context, ruleCtx *correlation.RuleContext) ([]correlation.Finding, error) {
	var findings []correlation.Finding

	// Get Kubernetes data
	k8sData, err := ruleCtx.DataCollection.GetKubernetesData(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes data: %w", err)
	}

	// Get additional data sources
	var metricsData *correlation.MetricsData
	var ebpfData *correlation.EBPFData

	if r.config.UseMetricsData && ruleCtx.DataCollection.IsSourceAvailable(correlation.SourceMetrics) {
		metricsData, err = ruleCtx.DataCollection.GetMetricsData(ctx)
		if err != nil {
			metricsData = nil
		}
	}

	if r.config.UseEBPFData && ruleCtx.DataCollection.IsSourceAvailable(correlation.SourceEBPF) {
		ebpfData, err = ruleCtx.DataCollection.GetEBPFData(ctx)
		if err != nil {
			ebpfData = nil
		}
	}

	// Analyze each pod for CPU throttling
	for _, pod := range k8sData.Pods {
		if pod.Status.Phase != "Running" {
			continue
		}

		podFindings := r.analyzePodCPUThrottling(pod, k8sData, metricsData, ebpfData)
		findings = append(findings, podFindings...)
	}

	return findings, nil
}

// CPUThrottlingAnalysis contains the results of CPU throttling analysis
type CPUThrottlingAnalysis struct {
	ThrottlingDetected   bool          `json:"throttling_detected"`
	ThrottledPercentage  float64       `json:"throttled_percentage"`
	PerformanceImpact    float64       `json:"performance_impact"`
	AverageThrottleTime  time.Duration `json:"average_throttle_time"`
	MaxThrottleTime      time.Duration `json:"max_throttle_time"`
	ThrottlingFrequency  float64       `json:"throttling_frequency"`
	CPULimit             float64       `json:"cpu_limit"`
	CPURequest           float64       `json:"cpu_request"`
	ActualCPUUsage       float64       `json:"actual_cpu_usage"`
	DesiredCPUUsage      float64       `json:"desired_cpu_usage"`
	ConfidenceLevel      float64       `json:"confidence_level"`
	DataSources          []string      `json:"data_sources"`
	ThrottlingIndicators []string      `json:"throttling_indicators"`
	ObservationPeriod    time.Duration `json:"observation_period"`
}

// analyzePodCPUThrottling analyzes a specific pod for CPU throttling
func (r *CPUThrottlingRule) analyzePodCPUThrottling(pod interface{}, k8sData *correlation.KubernetesData, metricsData *correlation.MetricsData, ebpfData *correlation.EBPFData) []correlation.Finding {
	var findings []correlation.Finding

	// Extract pod information
	podName := fmt.Sprintf("pod-%d", time.Now().UnixNano())
	podNamespace := "default"

	// Analyze CPU throttling patterns
	throttlingAnalysis := r.analyzeCPUThrottlingPatterns(podName, podNamespace, k8sData, metricsData, ebpfData)

	// Generate findings based on analysis
	if throttlingAnalysis.ConfidenceLevel >= r.config.MinConfidence {
		finding := r.createCPUThrottlingFinding(podName, podNamespace, throttlingAnalysis)
		findings = append(findings, *finding)
	}

	return findings
}

// analyzeCPUThrottlingPatterns analyzes CPU throttling from all available sources
func (r *CPUThrottlingRule) analyzeCPUThrottlingPatterns(podName, podNamespace string, k8sData *correlation.KubernetesData, metricsData *correlation.MetricsData, ebpfData *correlation.EBPFData) CPUThrottlingAnalysis {
	analysis := CPUThrottlingAnalysis{
		DataSources:          make([]string, 0),
		ThrottlingIndicators: make([]string, 0),
		ObservationPeriod:    r.config.MinObservationPeriod,
	}

	// Analyze metrics data if available
	if metricsData != nil {
		r.analyzeMetricsCPUThrottling(podName, podNamespace, metricsData, &analysis)
	}

	// Analyze eBPF data if available
	if ebpfData != nil {
		r.analyzeEBPFCPUThrottling(podName, ebpfData, &analysis)
	}

	// Analyze Kubernetes data
	r.analyzeKubernetesCPUThrottling(podName, podNamespace, k8sData, &analysis)

	// Calculate overall confidence
	analysis.ConfidenceLevel = r.calculateThrottlingConfidence(&analysis)

	// Determine if throttling is detected
	analysis.ThrottlingDetected = analysis.ConfidenceLevel >= r.config.MinConfidence &&
		analysis.ThrottledPercentage >= r.config.ThrottlingThreshold &&
		analysis.PerformanceImpact >= r.config.ImpactThreshold

	return analysis
}

// analyzeMetricsCPUThrottling analyzes metrics data for CPU throttling
func (r *CPUThrottlingRule) analyzeMetricsCPUThrottling(podName, podNamespace string, metricsData *correlation.MetricsData, analysis *CPUThrottlingAnalysis) {
	analysis.DataSources = append(analysis.DataSources, "metrics")

	// Find container metrics
	for _, containerMetrics := range metricsData.ContainerMetrics {
		if containerMetrics.PodName == podName && containerMetrics.Namespace == podNamespace {
			// Check CPU metrics
			if containerMetrics.CPU.ThrottledTime > 0 {
				analysis.ThrottlingIndicators = append(analysis.ThrottlingIndicators, "cpu_throttled_time")

				// Calculate throttled percentage
				if containerMetrics.CPU.TotalTime > 0 {
					analysis.ThrottledPercentage = float64(containerMetrics.CPU.ThrottledTime) / float64(containerMetrics.CPU.TotalTime)
				}
			}

			// Check if CPU usage is at limit
			if containerMetrics.CPU.Usage >= 0.95 {
				analysis.ThrottlingIndicators = append(analysis.ThrottlingIndicators, "cpu_at_limit")
			}

			analysis.CPULimit = containerMetrics.CPU.Limit
			analysis.CPURequest = containerMetrics.CPU.Request
			analysis.ActualCPUUsage = containerMetrics.CPU.Current

			// Performance impact estimation
			if containerMetrics.CPU.Limit > 0 && containerMetrics.CPU.Current > 0 {
				analysis.DesiredCPUUsage = containerMetrics.CPU.Current / (1 - analysis.ThrottledPercentage)
				analysis.PerformanceImpact = (analysis.DesiredCPUUsage - containerMetrics.CPU.Current) / analysis.DesiredCPUUsage
			}
		}
	}

	// Check pod-level metrics
	for _, podMetrics := range metricsData.PodMetrics {
		if podMetrics.Name == podName && podMetrics.Namespace == podNamespace {
			// Check for sustained high CPU usage
			if podMetrics.CPU.Usage > 0.9 && podMetrics.CPU.Trend > 0 {
				analysis.ThrottlingIndicators = append(analysis.ThrottlingIndicators, "sustained_high_cpu")
			}
		}
	}
}

// analyzeEBPFCPUThrottling analyzes eBPF data for CPU throttling
func (r *CPUThrottlingRule) analyzeEBPFCPUThrottling(podName string, ebpfData *correlation.EBPFData, analysis *CPUThrottlingAnalysis) {
	analysis.DataSources = append(analysis.DataSources, "eBPF")

	// Check system-wide CPU pressure
	if ebpfData.SystemMetrics.CPUPressure > 0.8 {
		analysis.ThrottlingIndicators = append(analysis.ThrottlingIndicators, "high_cpu_pressure")
	}

	// Analyze CPU events for throttling patterns
	throttleEvents := 0
	var totalThrottleTime time.Duration
	var maxThrottleTime time.Duration

	for _, event := range ebpfData.CPUEvents {
		if event.EventType == "throttle" {
			throttleEvents++
			throttleDuration := time.Duration(event.Duration)
			totalThrottleTime += throttleDuration

			if throttleDuration > maxThrottleTime {
				maxThrottleTime = throttleDuration
			}
		}
	}

	if throttleEvents > 0 {
		analysis.ThrottlingIndicators = append(analysis.ThrottlingIndicators, "cpu_throttle_events")
		analysis.AverageThrottleTime = totalThrottleTime / time.Duration(throttleEvents)
		analysis.MaxThrottleTime = maxThrottleTime
		analysis.ThrottlingFrequency = float64(throttleEvents) / analysis.ObservationPeriod.Minutes()
	}
}

// analyzeKubernetesCPUThrottling analyzes Kubernetes data for CPU throttling
func (r *CPUThrottlingRule) analyzeKubernetesCPUThrottling(podName, podNamespace string, k8sData *correlation.KubernetesData, analysis *CPUThrottlingAnalysis) {
	analysis.DataSources = append(analysis.DataSources, "kubernetes")

	// Check for performance-related events
	for _, event := range k8sData.Events {
		if event.InvolvedObject.Name == podName {
			switch event.Reason {
			case "CPUThrottlingHigh":
				analysis.ThrottlingIndicators = append(analysis.ThrottlingIndicators, "cpu_throttling_event")
			case "FailedScheduling":
				if event.Message != "" && (contains(event.Message, "Insufficient cpu") || contains(event.Message, "cpu")) {
					analysis.ThrottlingIndicators = append(analysis.ThrottlingIndicators, "insufficient_cpu_resources")
				}
			}
		}
	}

	// Check for performance problems
	for _, problem := range k8sData.Problems {
		if problem.Resource.Kind == "Pod" && problem.Resource.Name == podName {
			if string(problem.Severity) == "warning" || string(problem.Severity) == "critical" {
				analysis.ThrottlingIndicators = append(analysis.ThrottlingIndicators, "performance_problem_detected")
			}
		}
	}
}

// calculateThrottlingConfidence calculates the overall confidence level
func (r *CPUThrottlingRule) calculateThrottlingConfidence(analysis *CPUThrottlingAnalysis) float64 {
	var confidenceFactors []float64

	// Throttled percentage factor
	if analysis.ThrottledPercentage >= r.config.ThrottlingThreshold {
		throttleFactor := math.Min(analysis.ThrottledPercentage/r.config.ThrottlingThreshold, 2.0) / 2.0
		confidenceFactors = append(confidenceFactors, throttleFactor)
	}

	// Performance impact factor
	if analysis.PerformanceImpact >= r.config.ImpactThreshold {
		impactFactor := math.Min(analysis.PerformanceImpact/r.config.ImpactThreshold, 2.0) / 2.0
		confidenceFactors = append(confidenceFactors, impactFactor)
	}

	// Data source factor
	dataSourceFactor := float64(len(analysis.DataSources)) / 3.0
	confidenceFactors = append(confidenceFactors, dataSourceFactor)

	// Indicator factor
	if len(analysis.ThrottlingIndicators) > 0 {
		indicatorFactor := math.Min(float64(len(analysis.ThrottlingIndicators))/5.0, 1.0)
		confidenceFactors = append(confidenceFactors, indicatorFactor)
	}

	// Throttling frequency factor
	if analysis.ThrottlingFrequency > 0 {
		frequencyFactor := math.Min(analysis.ThrottlingFrequency/10.0, 1.0)
		confidenceFactors = append(confidenceFactors, frequencyFactor)
	}

	// Calculate weighted average
	if len(confidenceFactors) == 0 {
		return 0
	}

	var totalConfidence float64
	for _, factor := range confidenceFactors {
		totalConfidence += factor
	}

	return math.Min(totalConfidence/float64(len(confidenceFactors)), 1.0)
}

// createCPUThrottlingFinding creates a finding for CPU throttling
func (r *CPUThrottlingRule) createCPUThrottlingFinding(podName, podNamespace string, analysis CPUThrottlingAnalysis) *correlation.Finding {
	// Determine severity
	var severity correlation.Severity
	if analysis.PerformanceImpact >= 0.3 || analysis.ThrottledPercentage >= 0.5 {
		severity = correlation.SeverityLevelCritical
	} else if analysis.PerformanceImpact >= 0.2 || analysis.ThrottledPercentage >= 0.3 {
		severity = correlation.SeverityLevelError
	} else {
		severity = correlation.SeverityLevelWarning
	}

	// Create finding
	finding := r.CreateFinding(
		"CPU Throttling Detected",
		fmt.Sprintf("Pod %s/%s is experiencing CPU throttling affecting performance", podNamespace, podName),
		severity,
		analysis.ConfidenceLevel,
	)

	// Add resource reference
	finding.Resource = correlation.ResourceInfo{
		Type:      "pod",
		Name:      podName,
		Namespace: podNamespace,
	}

	// Add evidence
	for _, source := range analysis.DataSources {
		evidence := correlation.RuleEvidence{
			Type:        "cpu_throttling",
			Source:      correlation.SourceType(source),
			Description: fmt.Sprintf("CPU throttling analysis from %s", source),
			Data: map[string]interface{}{
				"throttled_percentage":  analysis.ThrottledPercentage,
				"performance_impact":    analysis.PerformanceImpact,
				"average_throttle_time": analysis.AverageThrottleTime,
				"max_throttle_time":     analysis.MaxThrottleTime,
				"throttling_frequency":  analysis.ThrottlingFrequency,
				"cpu_limit":             analysis.CPULimit,
				"cpu_request":           analysis.CPURequest,
				"actual_cpu_usage":      analysis.ActualCPUUsage,
				"desired_cpu_usage":     analysis.DesiredCPUUsage,
			},
			Timestamp:  time.Now(),
			Confidence: analysis.ConfidenceLevel,
		}
		finding.AddEvidence(evidence)
	}

	// Add tags
	finding.AddTag("cpu")
	finding.AddTag("throttling")
	finding.AddTag("performance")
	finding.AddTag("resource-limits")

	// Add metadata
	finding.SetMetadata("analysis", analysis)
	finding.SetMetadata("throttling_indicators", analysis.ThrottlingIndicators)
	finding.SetMetadata("recommended_cpu_limit", analysis.DesiredCPUUsage*1.2) // 20% headroom

	// Add recommendations
	finding.SetMetadata("recommendations", []string{
		fmt.Sprintf("Increase CPU limit to at least %.2f cores", analysis.DesiredCPUUsage*1.2),
		"Review application CPU usage patterns",
		"Consider horizontal scaling if CPU demand is legitimate",
		"Optimize application code for CPU efficiency",
		"Monitor CPU throttling metrics continuously",
	})

	return finding
}

// GetConfidenceFactors returns factors that affect confidence scoring
func (r *CPUThrottlingRule) GetConfidenceFactors() []string {
	return []string{
		"throttled_time_percentage",
		"performance_impact_measurement",
		"multiple_data_sources",
		"throttling_event_frequency",
		"cpu_limit_utilization",
		"throttling_indicators",
	}
}

// Validate validates the rule configuration
func (r *CPUThrottlingRule) Validate() error {
	if err := r.BaseRule.Validate(); err != nil {
		return err
	}

	if r.config.MinObservationPeriod <= 0 {
		return correlation.NewRuleValidationError("min_observation_period must be positive")
	}

	if r.config.MinDataPoints < 1 {
		return correlation.NewRuleValidationError("min_data_points must be at least 1")
	}

	if r.config.ThrottlingThreshold <= 0 || r.config.ThrottlingThreshold > 1 {
		return correlation.NewRuleValidationError("throttling_threshold must be between 0 and 1")
	}

	if r.config.ImpactThreshold <= 0 || r.config.ImpactThreshold > 1 {
		return correlation.NewRuleValidationError("impact_threshold must be between 0 and 1")
	}

	if r.config.MinConfidence <= 0 || r.config.MinConfidence > 1 {
		return correlation.NewRuleValidationError("min_confidence must be between 0 and 1")
	}

	return nil
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr || len(s) > len(substr) && containsHelper(s[1:], substr)
}

func containsHelper(s, substr string) bool {
	if len(s) < len(substr) {
		return false
	}
	if s[:len(substr)] == substr {
		return true
	}
	return containsHelper(s[1:], substr)
}
