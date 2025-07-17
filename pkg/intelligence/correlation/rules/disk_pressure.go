package rules
import (
	"context"
	"fmt"
	"math"
	"time"
	"github.com/falseyair/tapio/pkg/intelligence/correlation"
)
// DiskPressureRule detects disk pressure affecting performance and stability
type DiskPressureRule struct {
	*correlation.BaseRule
	config DiskPressureConfig
}
// DiskPressureConfig configures the disk pressure detection rule
type DiskPressureConfig struct {
	UsageThreshold        float64       `json:"usage_threshold"`         // Disk usage percentage threshold
	InodeThreshold        float64       `json:"inode_threshold"`         // Inode usage percentage threshold
	IOWaitThreshold       float64       `json:"iowait_threshold"`        // IO wait percentage threshold
	WriteLatencyThreshold time.Duration `json:"write_latency_threshold"` // Write latency threshold
	ReadLatencyThreshold  time.Duration `json:"read_latency_threshold"`  // Read latency threshold
	MinConfidence         float64       `json:"min_confidence"`
	PredictionWindow      time.Duration `json:"prediction_window"`
	UseEBPFData           bool          `json:"use_ebpf_data"`
	UseMetricsData        bool          `json:"use_metrics_data"`
	UseKubernetesData     bool          `json:"use_kubernetes_data"`
}
// DefaultDiskPressureConfig returns default configuration
func DefaultDiskPressureConfig() DiskPressureConfig {
	return DiskPressureConfig{
		UsageThreshold:        0.85, // 85% disk usage
		InodeThreshold:        0.90, // 90% inode usage
		IOWaitThreshold:       0.20, // 20% IO wait
		WriteLatencyThreshold: 100 * time.Millisecond,
		ReadLatencyThreshold:  50 * time.Millisecond,
		MinConfidence:         0.7,
		PredictionWindow:      24 * time.Hour,
		UseEBPFData:           true,
		UseMetricsData:        true,
		UseKubernetesData:     true,
	}
}
// NewDiskPressureRule creates a new disk pressure detection rule
func NewDiskPressureRule(config DiskPressureConfig) *DiskPressureRule {
	metadata := correlation.RuleMetadata{
		ID:          "disk_pressure_detection",
		Name:        "Disk Pressure Detection",
		Description: "Detects disk pressure that may affect application performance and stability",
		Version:     "1.0.0",
		Author:      "Tapio Correlation Engine",
		Tags:        []string{"disk", "storage", "performance", "pressure"},
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
				DataType:   "io_stats",
				Required:   false,
				Fallback:   "metrics_data",
			},
		},
		Enabled:   true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	return &DiskPressureRule{
		BaseRule: correlation.NewBaseRule(metadata),
		config:   config,
	}
}
// CheckRequirements verifies that required data sources are available
func (r *DiskPressureRule) CheckRequirements(ctx context.Context, data *correlation.DataCollection) error {
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
		return fmt.Errorf("disk pressure detection requires at least one additional data source (metrics or eBPF)")
	}
	return nil
}
// Execute runs the disk pressure detection rule
func (r *DiskPressureRule) Execute(ctx context.Context, ruleCtx *correlation.RuleContext) ([]correlation.Finding, error) {
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
	// Analyze node-level disk pressure
	nodeFindings := r.analyzeNodeDiskPressure(k8sData, metricsData, ebpfData)
	findings = append(findings, nodeFindings...)
	// Analyze pod-level disk pressure
	for _, pod := range k8sData.Pods {
		if pod.Status.Phase != "Running" {
			continue
		}
		podFindings := r.analyzePodDiskPressure(pod, k8sData, metricsData, ebpfData)
		findings = append(findings, podFindings...)
	}
	return findings, nil
}
// DiskPressureAnalysis contains the results of disk pressure analysis
type DiskPressureAnalysis struct {
	PressureDetected   bool          `json:"pressure_detected"`
	DiskUsagePercent   float64       `json:"disk_usage_percent"`
	InodeUsagePercent  float64       `json:"inode_usage_percent"`
	IOWaitPercent      float64       `json:"iowait_percent"`
	WriteLatency       time.Duration `json:"write_latency"`
	ReadLatency        time.Duration `json:"read_latency"`
	WriteIOPS          float64       `json:"write_iops"`
	ReadIOPS           float64       `json:"read_iops"`
	GrowthRate         float64       `json:"growth_rate"` // GB per day
	TimeToFull         time.Duration `json:"time_to_full"`
	ConfidenceLevel    float64       `json:"confidence_level"`
	DataSources        []string      `json:"data_sources"`
	PressureIndicators []string      `json:"pressure_indicators"`
	AffectedPaths      []string      `json:"affected_paths"`
	TopConsumers       []string      `json:"top_consumers"`
}
// analyzeNodeDiskPressure analyzes node-level disk pressure
func (r *DiskPressureRule) analyzeNodeDiskPressure(k8sData *correlation.KubernetesData, metricsData *correlation.MetricsData, ebpfData *correlation.EBPFData) []correlation.Finding {
	var findings []correlation.Finding
	// Analyze each node
	nodeName := "default-node" // Simplified for example
	analysis := r.analyzeDiskPressurePatterns(nodeName, "", k8sData, metricsData, ebpfData, true)
	if analysis.ConfidenceLevel >= r.config.MinConfidence {
		finding := r.createDiskPressureFinding(nodeName, "", "Node", analysis)
		findings = append(findings, *finding)
	}
	return findings
}
// analyzePodDiskPressure analyzes pod-level disk pressure
func (r *DiskPressureRule) analyzePodDiskPressure(pod interface{}, k8sData *correlation.KubernetesData, metricsData *correlation.MetricsData, ebpfData *correlation.EBPFData) []correlation.Finding {
	var findings []correlation.Finding
	// Extract pod information
	podName := fmt.Sprintf("pod-%d", time.Now().UnixNano())
	podNamespace := "default"
	// Analyze disk pressure patterns
	diskAnalysis := r.analyzeDiskPressurePatterns(podName, podNamespace, k8sData, metricsData, ebpfData, false)
	// Generate findings based on analysis
	if diskAnalysis.ConfidenceLevel >= r.config.MinConfidence {
		finding := r.createDiskPressureFinding(podName, podNamespace, "Pod", diskAnalysis)
		findings = append(findings, *finding)
	}
	return findings
}
// analyzeDiskPressurePatterns analyzes disk pressure from all available sources
func (r *DiskPressureRule) analyzeDiskPressurePatterns(resourceName, resourceNamespace string, k8sData *correlation.KubernetesData, metricsData *correlation.MetricsData, ebpfData *correlation.EBPFData, isNode bool) DiskPressureAnalysis {
	analysis := DiskPressureAnalysis{
		DataSources:        make([]string, 0),
		PressureIndicators: make([]string, 0),
		AffectedPaths:      make([]string, 0),
		TopConsumers:       make([]string, 0),
	}
	// Analyze metrics data if available
	if metricsData != nil {
		r.analyzeMetricsDiskPressure(resourceName, resourceNamespace, metricsData, &analysis, isNode)
	}
	// Analyze eBPF data if available
	if ebpfData != nil {
		r.analyzeEBPFDiskPressure(resourceName, ebpfData, &analysis)
	}
	// Analyze Kubernetes data
	r.analyzeKubernetesDiskPressure(resourceName, resourceNamespace, k8sData, &analysis, isNode)
	// Calculate time to full if growth rate is positive
	if analysis.GrowthRate > 0 && analysis.DiskUsagePercent < 1.0 {
		remainingCapacity := (1.0 - analysis.DiskUsagePercent) * 100 // Assuming 100GB for example
		daysToFull := remainingCapacity / analysis.GrowthRate
		analysis.TimeToFull = time.Duration(daysToFull*24) * time.Hour
	}
	// Calculate overall confidence
	analysis.ConfidenceLevel = r.calculateDiskPressureConfidence(&analysis)
	// Determine if pressure is detected
	analysis.PressureDetected = analysis.ConfidenceLevel >= r.config.MinConfidence &&
		(analysis.DiskUsagePercent >= r.config.UsageThreshold ||
			analysis.InodeUsagePercent >= r.config.InodeThreshold ||
			analysis.IOWaitPercent >= r.config.IOWaitThreshold ||
			analysis.WriteLatency >= r.config.WriteLatencyThreshold ||
			analysis.ReadLatency >= r.config.ReadLatencyThreshold)
	return analysis
}
// analyzeMetricsDiskPressure analyzes metrics data for disk pressure
func (r *DiskPressureRule) analyzeMetricsDiskPressure(resourceName, resourceNamespace string, metricsData *correlation.MetricsData, analysis *DiskPressureAnalysis, isNode bool) {
	analysis.DataSources = append(analysis.DataSources, "metrics")
	if isNode {
		// Analyze node metrics
		if metricsData.NodeMetrics.Name == resourceName || resourceName == "default-node" {
			// Disk usage
			if metricsData.NodeMetrics.DiskPressure {
				analysis.PressureIndicators = append(analysis.PressureIndicators, "node_disk_pressure")
			}
			// Simulated disk metrics
			analysis.DiskUsagePercent = 0.87 // 87% used
			if analysis.DiskUsagePercent >= r.config.UsageThreshold {
				analysis.PressureIndicators = append(analysis.PressureIndicators, "high_disk_usage")
			}
			// Inode usage
			analysis.InodeUsagePercent = 0.45 // 45% used
			if analysis.InodeUsagePercent >= r.config.InodeThreshold {
				analysis.PressureIndicators = append(analysis.PressureIndicators, "high_inode_usage")
			}
		}
	} else {
		// Analyze pod/container metrics
		for _, containerMetrics := range metricsData.ContainerMetrics {
			if containerMetrics.PodName == resourceName && containerMetrics.Namespace == resourceNamespace {
				// Volume usage
				if containerMetrics.VolumeUsage > 0.8 {
					analysis.PressureIndicators = append(analysis.PressureIndicators, "high_volume_usage")
					analysis.AffectedPaths = append(analysis.AffectedPaths, containerMetrics.VolumePath)
				}
				// IO metrics
				analysis.WriteIOPS = containerMetrics.WriteIOPS
				analysis.ReadIOPS = containerMetrics.ReadIOPS
				if containerMetrics.WriteIOPS > 1000 {
					analysis.PressureIndicators = append(analysis.PressureIndicators, "high_write_iops")
				}
			}
		}
	}
	// Growth rate calculation (simplified)
	analysis.GrowthRate = 2.5 // 2.5 GB per day
}
// analyzeEBPFDiskPressure analyzes eBPF data for disk pressure
func (r *DiskPressureRule) analyzeEBPFDiskPressure(resourceName string, ebpfData *correlation.EBPFData, analysis *DiskPressureAnalysis) {
	analysis.DataSources = append(analysis.DataSources, "eBPF")
	// Analyze IO events
	var totalWriteLatency time.Duration
	var totalReadLatency time.Duration
	writeCount := 0
	readCount := 0
	for _, ioEvent := range ebpfData.IOEvents {
		if ioEvent.EventType == "write" {
			totalWriteLatency += time.Duration(ioEvent.Latency)
			writeCount++
		} else if ioEvent.EventType == "read" {
			totalReadLatency += time.Duration(ioEvent.Latency)
			readCount++
		}
		// Check for slow IO
		if time.Duration(ioEvent.Latency) > 500*time.Millisecond {
			analysis.PressureIndicators = append(analysis.PressureIndicators, "slow_io_operations")
		}
	}
	// Calculate average latencies
	if writeCount > 0 {
		analysis.WriteLatency = totalWriteLatency / time.Duration(writeCount)
		if analysis.WriteLatency >= r.config.WriteLatencyThreshold {
			analysis.PressureIndicators = append(analysis.PressureIndicators, "high_write_latency")
		}
	}
	if readCount > 0 {
		analysis.ReadLatency = totalReadLatency / time.Duration(readCount)
		if analysis.ReadLatency >= r.config.ReadLatencyThreshold {
			analysis.PressureIndicators = append(analysis.PressureIndicators, "high_read_latency")
		}
	}
	// Check system IO wait
	analysis.IOWaitPercent = ebpfData.SystemMetrics.IOWait
	if analysis.IOWaitPercent >= r.config.IOWaitThreshold {
		analysis.PressureIndicators = append(analysis.PressureIndicators, "high_iowait")
	}
	// Identify top consumers
	for _, processStats := range ebpfData.ProcessStats {
		if processStats.IOBytesWritten > 1000000000 { // 1GB
			analysis.TopConsumers = append(analysis.TopConsumers, processStats.Command)
		}
	}
}
// analyzeKubernetesDiskPressure analyzes Kubernetes data for disk pressure
func (r *DiskPressureRule) analyzeKubernetesDiskPressure(resourceName, resourceNamespace string, k8sData *correlation.KubernetesData, analysis *DiskPressureAnalysis, isNode bool) {
	analysis.DataSources = append(analysis.DataSources, "kubernetes")
	// Check for disk-related events
	for _, event := range k8sData.Events {
		if (isNode && event.InvolvedObject.Kind == "Node" && event.InvolvedObject.Name == resourceName) ||
			(!isNode && event.InvolvedObject.Kind == "Pod" && event.InvolvedObject.Name == resourceName) {
			switch event.Reason {
			case "DiskPressure":
				analysis.PressureIndicators = append(analysis.PressureIndicators, "disk_pressure_event")
			case "EvictedByDiskPressure":
				analysis.PressureIndicators = append(analysis.PressureIndicators, "eviction_by_disk_pressure")
			case "VolumeResizeFailed":
				analysis.PressureIndicators = append(analysis.PressureIndicators, "volume_resize_failed")
			}
		}
	}
	// Check for disk-related problems
	for _, problem := range k8sData.Problems {
		if problem.Resource.Name == resourceName {
			if string(problem.Severity) == "critical" || string(problem.Severity) == "error" {
				analysis.PressureIndicators = append(analysis.PressureIndicators, "disk_problem_detected")
			}
		}
	}
}
// calculateDiskPressureConfidence calculates the overall confidence level
func (r *DiskPressureRule) calculateDiskPressureConfidence(analysis *DiskPressureAnalysis) float64 {
	var confidenceFactors []float64
	// Disk usage factor
	if analysis.DiskUsagePercent >= r.config.UsageThreshold {
		usageFactor := math.Min((analysis.DiskUsagePercent-r.config.UsageThreshold)/(1.0-r.config.UsageThreshold), 1.0)
		confidenceFactors = append(confidenceFactors, usageFactor)
	}
	// Inode usage factor
	if analysis.InodeUsagePercent >= r.config.InodeThreshold {
		inodeFactor := math.Min((analysis.InodeUsagePercent-r.config.InodeThreshold)/(1.0-r.config.InodeThreshold), 1.0)
		confidenceFactors = append(confidenceFactors, inodeFactor)
	}
	// IO wait factor
	if analysis.IOWaitPercent >= r.config.IOWaitThreshold {
		ioWaitFactor := math.Min(analysis.IOWaitPercent/r.config.IOWaitThreshold, 1.0)
		confidenceFactors = append(confidenceFactors, ioWaitFactor)
	}
	// Latency factors
	if analysis.WriteLatency >= r.config.WriteLatencyThreshold {
		writeLatencyFactor := math.Min(float64(analysis.WriteLatency)/float64(r.config.WriteLatencyThreshold), 1.0)
		confidenceFactors = append(confidenceFactors, writeLatencyFactor)
	}
	if analysis.ReadLatency >= r.config.ReadLatencyThreshold {
		readLatencyFactor := math.Min(float64(analysis.ReadLatency)/float64(r.config.ReadLatencyThreshold), 1.0)
		confidenceFactors = append(confidenceFactors, readLatencyFactor)
	}
	// Indicator factor
	if len(analysis.PressureIndicators) > 0 {
		indicatorFactor := math.Min(float64(len(analysis.PressureIndicators))/5.0, 1.0)
		confidenceFactors = append(confidenceFactors, indicatorFactor)
	}
	// Data source factor
	dataSourceFactor := float64(len(analysis.DataSources)) / 3.0
	confidenceFactors = append(confidenceFactors, dataSourceFactor)
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
// createDiskPressureFinding creates a finding for disk pressure
func (r *DiskPressureRule) createDiskPressureFinding(resourceName, resourceNamespace, resourceKind string, analysis DiskPressureAnalysis) *correlation.Finding {
	// Determine severity
	var severity correlation.Severity
	if analysis.DiskUsagePercent >= 0.95 || analysis.InodeUsagePercent >= 0.95 ||
		(analysis.TimeToFull > 0 && analysis.TimeToFull < 24*time.Hour) {
		severity = correlation.SeverityLevelCritical
	} else if analysis.DiskUsagePercent >= 0.90 || analysis.InodeUsagePercent >= 0.90 ||
		(analysis.TimeToFull > 0 && analysis.TimeToFull < 7*24*time.Hour) {
		severity = correlation.SeverityLevelError
	} else {
		severity = correlation.SeverityLevelWarning
	}
	// Create finding
	title := fmt.Sprintf("Disk Pressure Detected on %s", resourceKind)
	description := fmt.Sprintf("%s %s is experiencing disk pressure with %.0f%% disk usage",
		resourceKind, resourceName, analysis.DiskUsagePercent*100)
	finding := r.CreateFinding(
		title,
		description,
		severity,
		analysis.ConfidenceLevel,
	)
	// Add resource reference
	finding.Resource = correlation.ResourceInfo{
		Type:      resourceKind,
		Name:      resourceName,
		Namespace: resourceNamespace,
	}
	// Add prediction if time to full is calculated
	if analysis.TimeToFull > 0 {
		finding.Prediction = &correlation.RulePrediction{
			Event:       "Disk Full",
			TimeToEvent: analysis.TimeToFull,
			Confidence:  analysis.ConfidenceLevel,
			Factors:     analysis.PressureIndicators,
			Mitigation: []string{
				"Clean up unnecessary files and logs",
				"Increase disk/volume size",
				"Implement log rotation and retention policies",
				"Move data to external storage",
				"Identify and remove large temporary files",
			},
			UpdatedAt: time.Now(),
		}
	}
	// Add evidence
	for _, source := range analysis.DataSources {
		evidence := correlation.RuleEvidence{
			Type:        "disk_pressure",
			Source:      correlation.SourceType(source),
			Description: fmt.Sprintf("Disk pressure analysis from %s", source),
			Data: map[string]interface{}{
				"disk_usage_percent":  analysis.DiskUsagePercent,
				"inode_usage_percent": analysis.InodeUsagePercent,
				"iowait_percent":      analysis.IOWaitPercent,
				"write_latency":       analysis.WriteLatency,
				"read_latency":        analysis.ReadLatency,
				"write_iops":          analysis.WriteIOPS,
				"read_iops":           analysis.ReadIOPS,
				"growth_rate":         analysis.GrowthRate,
				"affected_paths":      analysis.AffectedPaths,
			},
			Timestamp:  time.Now(),
			Confidence: analysis.ConfidenceLevel,
		}
		finding.AddEvidence(evidence)
	}
	// Add tags
	finding.AddTag("disk")
	finding.AddTag("storage")
	finding.AddTag("pressure")
	finding.AddTag("performance")
	// Add metadata
	finding.SetMetadata("analysis", analysis)
	finding.SetMetadata("pressure_indicators", analysis.PressureIndicators)
	finding.SetMetadata("top_consumers", analysis.TopConsumers)
	// Add specific recommendations based on the type of pressure
	recommendations := make([]string, 0)
	if analysis.DiskUsagePercent >= r.config.UsageThreshold {
		recommendations = append(recommendations,
			fmt.Sprintf("Disk usage at %.0f%% - clean up or expand storage", analysis.DiskUsagePercent*100))
	}
	if analysis.InodeUsagePercent >= r.config.InodeThreshold {
		recommendations = append(recommendations,
			fmt.Sprintf("Inode usage at %.0f%% - remove small files or increase inode limit", analysis.InodeUsagePercent*100))
	}
	if analysis.IOWaitPercent >= r.config.IOWaitThreshold {
		recommendations = append(recommendations,
			fmt.Sprintf("IO wait at %.0f%% - optimize IO operations or upgrade storage", analysis.IOWaitPercent*100))
	}
	if len(analysis.TopConsumers) > 0 {
		recommendations = append(recommendations,
			fmt.Sprintf("Top disk consumers: %v - investigate these processes", analysis.TopConsumers))
	}
	finding.SetMetadata("recommendations", recommendations)
	return finding
}
// GetConfidenceFactors returns factors that affect confidence scoring
func (r *DiskPressureRule) GetConfidenceFactors() []string {
	return []string{
		"disk_usage_percentage",
		"inode_usage_percentage",
		"io_wait_percentage",
		"write_latency",
		"read_latency",
		"multiple_data_sources",
		"pressure_indicators",
		"growth_rate_analysis",
	}
}
// Validate validates the rule configuration
func (r *DiskPressureRule) Validate() error {
	if err := r.BaseRule.Validate(); err != nil {
		return err
	}
	if r.config.UsageThreshold <= 0 || r.config.UsageThreshold > 1 {
		return correlation.NewRuleValidationError("usage_threshold must be between 0 and 1")
	}
	if r.config.InodeThreshold <= 0 || r.config.InodeThreshold > 1 {
		return correlation.NewRuleValidationError("inode_threshold must be between 0 and 1")
	}
	if r.config.IOWaitThreshold <= 0 || r.config.IOWaitThreshold > 1 {
		return correlation.NewRuleValidationError("iowait_threshold must be between 0 and 1")
	}
	if r.config.WriteLatencyThreshold <= 0 {
		return correlation.NewRuleValidationError("write_latency_threshold must be positive")
	}
	if r.config.ReadLatencyThreshold <= 0 {
		return correlation.NewRuleValidationError("read_latency_threshold must be positive")
	}
	if r.config.MinConfidence <= 0 || r.config.MinConfidence > 1 {
		return correlation.NewRuleValidationError("min_confidence must be between 0 and 1")
	}
	if r.config.PredictionWindow <= 0 {
		return correlation.NewRuleValidationError("prediction_window must be positive")
	}
	return nil
}
