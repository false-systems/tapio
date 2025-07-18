package rules

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/yairfalse/tapio/pkg/correlation"
)

// OOMPredictionRule predicts Out-of-Memory events using multiple data sources
type OOMPredictionRule struct {
	*correlation.BaseRule
	config OOMPredictionConfig
}

// OOMPredictionConfig configures the OOM prediction rule
type OOMPredictionConfig struct {
	MinDataPoints       int           `json:"min_data_points"`
	PredictionWindow    time.Duration `json:"prediction_window"`
	CriticalThreshold   float64       `json:"critical_threshold"`    // Memory usage threshold for critical alerts
	WarningThreshold    float64       `json:"warning_threshold"`     // Memory usage threshold for warnings
	MinConfidence       float64       `json:"min_confidence"`        // Minimum confidence to report findings
	GrowthRateThreshold float64       `json:"growth_rate_threshold"` // Minimum growth rate to trigger prediction
	UseEBPFData         bool          `json:"use_ebpf_data"`
	UseMetricsData      bool          `json:"use_metrics_data"`
	UseKubernetesData   bool          `json:"use_kubernetes_data"`
}

// DefaultOOMPredictionConfig returns default configuration for OOM prediction
func DefaultOOMPredictionConfig() OOMPredictionConfig {
	return OOMPredictionConfig{
		MinDataPoints:       5,
		PredictionWindow:    time.Hour,
		CriticalThreshold:   0.9,  // 90%
		WarningThreshold:    0.8,  // 80%
		MinConfidence:       0.6,  // 60%
		GrowthRateThreshold: 0.05, // 5% per minute
		UseEBPFData:         true,
		UseMetricsData:      true,
		UseKubernetesData:   true,
	}
}

// NewOOMPredictionRule creates a new OOM prediction rule
func NewOOMPredictionRule(config OOMPredictionConfig) *OOMPredictionRule {
	metadata := correlation.RuleMetadata{
		ID:          "oom_prediction",
		Name:        "Out-of-Memory Prediction",
		Description: "Predicts OOM events using memory usage patterns from multiple sources",
		Version:     "1.0.0",
		Author:      "Tapio Correlation Engine",
		Tags:        []string{"memory", "prediction", "oom", "performance"},
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

	return &OOMPredictionRule{
		BaseRule: correlation.NewBaseRule(metadata),
		config:   config,
	}
}

// CheckRequirements verifies that required data sources are available
func (r *OOMPredictionRule) CheckRequirements(ctx context.Context, data *correlation.DataCollection) error {
	// Kubernetes data is required
	if !data.IsSourceAvailable(correlation.SourceKubernetes) {
		return correlation.NewRequirementNotMetError(r.GetMetadata().ID, r.GetMetadata().Requirements[0])
	}

	// At least one additional data source is needed for accurate prediction
	hasAdditionalSource := false
	if r.config.UseEBPFData && data.IsSourceAvailable(correlation.SourceEBPF) {
		hasAdditionalSource = true
	}
	if r.config.UseMetricsData && data.IsSourceAvailable(correlation.SourceMetrics) {
		hasAdditionalSource = true
	}

	if !hasAdditionalSource {
		return fmt.Errorf("OOM prediction requires at least one additional data source (eBPF or metrics)")
	}

	return nil
}

// Execute runs the OOM prediction rule
func (r *OOMPredictionRule) Execute(ctx context.Context, ruleCtx *correlation.RuleContext) ([]correlation.Finding, error) {
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
			// Log error but continue with other sources
			ebpfData = nil
		}
	}

	if r.config.UseMetricsData && ruleCtx.DataCollection.IsSourceAvailable(correlation.SourceMetrics) {
		metricsData, err = ruleCtx.DataCollection.GetMetricsData(ctx)
		if err != nil {
			// Log error but continue with other sources
			metricsData = nil
		}
	}

	// Analyze each pod for OOM risk
	for _, pod := range k8sData.Pods {
		if pod.Status.Phase != "Running" {
			continue // Skip non-running pods
		}

		podFindings := r.analyzePodOOMRisk(pod, k8sData, ebpfData, metricsData)
		findings = append(findings, podFindings...)
	}

	return findings, nil
}

// analyzePodOOMRisk analyzes a specific pod for OOM risk
func (r *OOMPredictionRule) analyzePodOOMRisk(pod interface{}, k8sData *correlation.KubernetesData, ebpfData *correlation.EBPFData, metricsData *correlation.MetricsData) []correlation.Finding {
	var findings []correlation.Finding

	// Extract pod information (simplified for this example)
	podName := fmt.Sprintf("pod-%s", uuid.New().String()) // Placeholder
	podNamespace := "default"                             // Placeholder

	// Analyze memory usage from different sources
	memoryAnalysis := r.analyzeMemoryUsage(podName, podNamespace, k8sData, ebpfData, metricsData)

	// Generate findings based on analysis
	if memoryAnalysis.RiskLevel >= r.config.MinConfidence {
		finding := r.createOOMFinding(podName, podNamespace, memoryAnalysis)
		findings = append(findings, *finding)
	}

	return findings
}

// MemoryAnalysis contains the results of memory usage analysis
type MemoryAnalysis struct {
	CurrentUsage      float64
	MemoryLimit       float64
	UsagePercent      float64
	GrowthRate        float64
	RiskLevel         float64
	TimeToOOM         time.Duration
	DataSources       []string
	ConfidenceFactors []string
}

// analyzeMemoryUsage analyzes memory usage from all available sources
func (r *OOMPredictionRule) analyzeMemoryUsage(podName, podNamespace string, k8sData *correlation.KubernetesData, ebpfData *correlation.EBPFData, metricsData *correlation.MetricsData) MemoryAnalysis {
	analysis := MemoryAnalysis{
		DataSources:       make([]string, 0),
		ConfidenceFactors: make([]string, 0),
	}

	// Analyze eBPF data if available
	if ebpfData != nil {
		r.analyzeEBPFMemory(podName, ebpfData, &analysis)
	}

	// Analyze metrics data if available
	if metricsData != nil {
		r.analyzeMetricsMemory(podName, podNamespace, metricsData, &analysis)
	}

	// Analyze Kubernetes data
	r.analyzeKubernetesMemory(podName, podNamespace, k8sData, &analysis)

	// Calculate overall risk level
	analysis.RiskLevel = r.calculateRiskLevel(analysis)

	// Calculate time to OOM if trend continues
	if analysis.GrowthRate > 0 && analysis.MemoryLimit > 0 {
		remainingMemory := analysis.MemoryLimit - analysis.CurrentUsage
		if remainingMemory > 0 {
			analysis.TimeToOOM = time.Duration(remainingMemory/analysis.GrowthRate) * time.Minute
		}
	}

	return analysis
}

// analyzeEBPFMemory analyzes eBPF memory data
func (r *OOMPredictionRule) analyzeEBPFMemory(podName string, ebpfData *correlation.EBPFData, analysis *MemoryAnalysis) {
	analysis.DataSources = append(analysis.DataSources, "eBPF")

	// Find relevant processes for this pod
	var totalUsage uint64
	var growthRates []float64

	for _, processStats := range ebpfData.ProcessStats {
		if processStats.InContainer {
			totalUsage += processStats.CurrentUsage

			// Calculate growth rate from pattern
			if len(processStats.GrowthPattern) >= r.config.MinDataPoints {
				growthRate := r.calculateGrowthRate(processStats.GrowthPattern)
				growthRates = append(growthRates, growthRate)
			}
		}
	}

	analysis.CurrentUsage = float64(totalUsage)
	analysis.ConfidenceFactors = append(analysis.ConfidenceFactors, "kernel_level_monitoring")

	// Calculate average growth rate
	if len(growthRates) > 0 {
		var totalGrowthRate float64
		for _, rate := range growthRates {
			totalGrowthRate += rate
		}
		analysis.GrowthRate = totalGrowthRate / float64(len(growthRates))
		analysis.ConfidenceFactors = append(analysis.ConfidenceFactors, "growth_pattern_analysis")
	}
}

// analyzeMetricsMemory analyzes metrics data
func (r *OOMPredictionRule) analyzeMetricsMemory(podName, podNamespace string, metricsData *correlation.MetricsData, analysis *MemoryAnalysis) {
	analysis.DataSources = append(analysis.DataSources, "metrics")

	// Find pod metrics
	for _, podMetrics := range metricsData.PodMetrics {
		if podMetrics.Name == podName && podMetrics.Namespace == podNamespace {
			analysis.CurrentUsage = podMetrics.Memory.Current
			analysis.MemoryLimit = podMetrics.Memory.Limit
			analysis.UsagePercent = podMetrics.Memory.Usage
			analysis.GrowthRate = podMetrics.Memory.Trend

			analysis.ConfidenceFactors = append(analysis.ConfidenceFactors, "metrics_monitoring")
			break
		}
	}
}

// analyzeKubernetesMemory analyzes Kubernetes data
func (r *OOMPredictionRule) analyzeKubernetesMemory(podName, podNamespace string, k8sData *correlation.KubernetesData, analysis *MemoryAnalysis) {
	analysis.DataSources = append(analysis.DataSources, "kubernetes")

	// This is a simplified analysis - in real implementation, you'd parse actual pod specs
	// and resource limits from the Kubernetes API data
	analysis.ConfidenceFactors = append(analysis.ConfidenceFactors, "kubernetes_resource_limits")
}

// calculateGrowthRate calculates the growth rate from memory data points
func (r *OOMPredictionRule) calculateGrowthRate(dataPoints []correlation.MemoryDataPoint) float64 {
	if len(dataPoints) < 2 {
		return 0
	}

	// Sort by timestamp
	sort.Slice(dataPoints, func(i, j int) bool {
		return dataPoints[i].Timestamp.Before(dataPoints[j].Timestamp)
	})

	// Calculate linear regression slope
	n := float64(len(dataPoints))
	var sumX, sumY, sumXY, sumX2 float64

	for i, point := range dataPoints {
		x := float64(i)
		y := float64(point.Usage)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	// Linear regression formula: slope = (n*sumXY - sumX*sumY) / (n*sumX2 - sumX*sumX)
	slope := (n*sumXY - sumX*sumY) / (n*sumX2 - sumX*sumX)

	// Convert to bytes per minute
	if len(dataPoints) > 1 {
		timeSpan := dataPoints[len(dataPoints)-1].Timestamp.Sub(dataPoints[0].Timestamp)
		if timeSpan > 0 {
			return slope * float64(time.Minute) / float64(timeSpan)
		}
	}

	return slope
}

// calculateRiskLevel calculates the overall OOM risk level
func (r *OOMPredictionRule) calculateRiskLevel(analysis MemoryAnalysis) float64 {
	var riskFactors []float64

	// Usage percentage risk
	if analysis.MemoryLimit > 0 && analysis.CurrentUsage > 0 {
		usagePercent := analysis.CurrentUsage / analysis.MemoryLimit
		riskFactors = append(riskFactors, usagePercent)
	}

	// Growth rate risk
	if analysis.GrowthRate > r.config.GrowthRateThreshold {
		growthRisk := math.Min(analysis.GrowthRate/r.config.GrowthRateThreshold, 1.0)
		riskFactors = append(riskFactors, growthRisk)
	}

	// Time to OOM risk
	if analysis.TimeToOOM > 0 && analysis.TimeToOOM < r.config.PredictionWindow {
		timeRisk := 1.0 - (float64(analysis.TimeToOOM) / float64(r.config.PredictionWindow))
		riskFactors = append(riskFactors, timeRisk)
	}

	// Data source confidence boost
	dataSourceMultiplier := 1.0
	if len(analysis.DataSources) > 1 {
		dataSourceMultiplier = 1.0 + (float64(len(analysis.DataSources)-1) * 0.1)
	}

	// Calculate weighted average
	if len(riskFactors) == 0 {
		return 0
	}

	var totalRisk float64
	for _, risk := range riskFactors {
		totalRisk += risk
	}

	averageRisk := totalRisk / float64(len(riskFactors))
	return math.Min(averageRisk*dataSourceMultiplier, 1.0)
}

// createOOMFinding creates a finding for OOM risk
func (r *OOMPredictionRule) createOOMFinding(podName, podNamespace string, analysis MemoryAnalysis) *correlation.Finding {
	// Determine severity based on risk level
	var severity correlation.Severity
	if analysis.RiskLevel >= r.config.CriticalThreshold {
		severity = correlation.SeverityLevelCritical
	} else if analysis.RiskLevel >= r.config.WarningThreshold {
		severity = correlation.SeverityLevelWarning
	} else {
		severity = correlation.SeverityLevelInfo
	}

	// Create finding
	finding := r.CreateFinding(
		"High OOM Risk Detected",
		fmt.Sprintf("Pod %s/%s shows high risk of out-of-memory condition", podNamespace, podName),
		severity,
		analysis.RiskLevel,
	)

	// Add resource reference
	finding.Resource = correlation.ResourceInfo{
		Type:      "Pod",
		Name:      podName,
		Namespace: podNamespace,
	}

	// Add prediction if time to OOM is calculated
	if analysis.TimeToOOM > 0 {
		finding.Prediction = &correlation.RulePrediction{
			Event:       "Out of Memory (OOM) Kill",
			TimeToEvent: analysis.TimeToOOM,
			Confidence:  analysis.RiskLevel,
			Factors:     analysis.ConfidenceFactors,
			Mitigation: []string{
				"Increase memory limits for the pod",
				"Optimize application memory usage",
				"Scale horizontally to distribute load",
				"Add memory monitoring alerts",
			},
			UpdatedAt: time.Now(),
		}
	}

	// Add evidence from different sources
	for _, source := range analysis.DataSources {
		evidence := correlation.RuleEvidence{
			Type:        "memory_usage",
			Source:      correlation.SourceType(source),
			Description: fmt.Sprintf("Memory usage data from %s", source),
			Data: map[string]interface{}{
				"current_usage": analysis.CurrentUsage,
				"memory_limit":  analysis.MemoryLimit,
				"usage_percent": analysis.UsagePercent,
				"growth_rate":   analysis.GrowthRate,
			},
			Timestamp:  time.Now(),
			Confidence: analysis.RiskLevel,
		}
		finding.AddEvidence(evidence)
	}

	// Add tags
	finding.AddTag("memory")
	finding.AddTag("oom")
	finding.AddTag("prediction")
	finding.AddTag("performance")

	// Add metadata
	finding.SetMetadata("analysis", analysis)
	finding.SetMetadata("prediction_window", r.config.PredictionWindow)
	finding.SetMetadata("data_sources", analysis.DataSources)

	return finding
}

// GetConfidenceFactors returns factors that affect confidence scoring
func (r *OOMPredictionRule) GetConfidenceFactors() []string {
	return []string{
		"multiple_data_sources",
		"growth_pattern_consistency",
		"memory_limit_availability",
		"historical_data_points",
		"kernel_level_monitoring",
		"metrics_accuracy",
	}
}

// Validate validates the rule configuration
func (r *OOMPredictionRule) Validate() error {
	if err := r.BaseRule.Validate(); err != nil {
		return err
	}

	if r.config.MinDataPoints < 2 {
		return correlation.NewRuleValidationError("min_data_points must be at least 2")
	}

	if r.config.PredictionWindow <= 0 {
		return correlation.NewRuleValidationError("prediction_window must be positive")
	}

	if r.config.CriticalThreshold <= r.config.WarningThreshold {
		return correlation.NewRuleValidationError("critical_threshold must be greater than warning_threshold")
	}

	if r.config.MinConfidence <= 0 || r.config.MinConfidence > 1 {
		return correlation.NewRuleValidationError("min_confidence must be between 0 and 1")
	}

	return nil
}
