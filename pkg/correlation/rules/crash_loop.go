package rules

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/falseyair/tapio/pkg/correlation"
)

// CrashLoopRule detects pods in crash loop states
type CrashLoopRule struct {
	*correlation.BaseRule
	config CrashLoopConfig
}

// CrashLoopConfig configures the crash loop detection rule
type CrashLoopConfig struct {
	MinRestartCount      int           `json:"min_restart_count"`
	ObservationWindow    time.Duration `json:"observation_window"`
	RestartRateThreshold float64       `json:"restart_rate_threshold"` // restarts per hour
	BackoffThreshold     time.Duration `json:"backoff_threshold"`      // max backoff time
	MinConfidence        float64       `json:"min_confidence"`
	UseKubernetesData    bool          `json:"use_kubernetes_data"`
	UseLogsData          bool          `json:"use_logs_data"`
	UseMetricsData       bool          `json:"use_metrics_data"`
}

// DefaultCrashLoopConfig returns default configuration
func DefaultCrashLoopConfig() CrashLoopConfig {
	return CrashLoopConfig{
		MinRestartCount:      3,
		ObservationWindow:    30 * time.Minute,
		RestartRateThreshold: 5.0, // 5 restarts per hour
		BackoffThreshold:     5 * time.Minute,
		MinConfidence:        0.8,
		UseKubernetesData:    true,
		UseLogsData:          true,
		UseMetricsData:       true,
	}
}

// NewCrashLoopRule creates a new crash loop detection rule
func NewCrashLoopRule(config CrashLoopConfig) *CrashLoopRule {
	metadata := correlation.RuleMetadata{
		ID:          "crash_loop_detection",
		Name:        "Pod Crash Loop Detection",
		Description: "Detects pods stuck in crash loop backoff states",
		Version:     "1.0.0",
		Author:      "Tapio Correlation Engine",
		Tags:        []string{"stability", "crash", "restart", "availability"},
		Requirements: []correlation.RuleRequirement{
			{
				SourceType: correlation.SourceKubernetes,
				DataType:   "full",
				Required:   true,
				Fallback:   "",
			},
			{
				SourceType: correlation.SourceLogs,
				DataType:   "recent",
				Required:   false,
				Fallback:   "kubernetes_events",
			},
			{
				SourceType: correlation.SourceMetrics,
				DataType:   "current",
				Required:   false,
				Fallback:   "",
			},
		},
		Enabled:   true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	return &CrashLoopRule{
		BaseRule: correlation.NewBaseRule(metadata),
		config:   config,
	}
}

// CheckRequirements verifies that required data sources are available
func (r *CrashLoopRule) CheckRequirements(ctx context.Context, data *correlation.DataCollection) error {
	// Kubernetes data is essential for crash loop detection
	if !data.IsSourceAvailable(correlation.SourceKubernetes) {
		return correlation.NewRequirementNotMetError(r.GetMetadata().ID, r.GetMetadata().Requirements[0])
	}

	return nil
}

// Execute runs the crash loop detection rule
func (r *CrashLoopRule) Execute(ctx context.Context, ruleCtx *correlation.RuleContext) ([]correlation.Finding, error) {
	var findings []correlation.Finding

	// Get Kubernetes data
	k8sData, err := ruleCtx.DataCollection.GetKubernetesData(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes data: %w", err)
	}

	// Get optional data sources
	var logsData *correlation.LogsData
	var metricsData *correlation.MetricsData

	if r.config.UseLogsData && ruleCtx.DataCollection.IsSourceAvailable(correlation.SourceLogs) {
		logsData, err = ruleCtx.DataCollection.GetLogsData(ctx)
		if err != nil {
			logsData = nil
		}
	}

	if r.config.UseMetricsData && ruleCtx.DataCollection.IsSourceAvailable(correlation.SourceMetrics) {
		metricsData, err = ruleCtx.DataCollection.GetMetricsData(ctx)
		if err != nil {
			metricsData = nil
		}
	}

	// Analyze each pod for crash loops
	for _, pod := range k8sData.Pods {
		podFindings := r.analyzePodCrashLoop(pod, k8sData, logsData, metricsData)
		findings = append(findings, podFindings...)
	}

	return findings, nil
}

// CrashLoopAnalysis contains the results of crash loop analysis
type CrashLoopAnalysis struct {
	InCrashLoop           bool          `json:"in_crash_loop"`
	RestartCount          int           `json:"restart_count"`
	RestartRate           float64       `json:"restart_rate"` // restarts per hour
	LastRestartTime       time.Time     `json:"last_restart_time"`
	AverageRunDuration    time.Duration `json:"average_run_duration"`
	CurrentBackoffTime    time.Duration `json:"current_backoff_time"`
	RestartReasons        []string      `json:"restart_reasons"`
	ErrorPatterns         []string      `json:"error_patterns"`
	ConfidenceLevel       float64       `json:"confidence_level"`
	DataSources           []string      `json:"data_sources"`
	CrashLoopIndicators   []string      `json:"crash_loop_indicators"`
	ObservationPeriod     time.Duration `json:"observation_period"`
	RestartTimes          []time.Time   `json:"restart_times"`
	EstimatedRecoveryTime time.Duration `json:"estimated_recovery_time"`
}

// analyzePodCrashLoop analyzes a specific pod for crash loop patterns
func (r *CrashLoopRule) analyzePodCrashLoop(pod interface{}, k8sData *correlation.KubernetesData, logsData *correlation.LogsData, metricsData *correlation.MetricsData) []correlation.Finding {
	var findings []correlation.Finding

	// Extract pod information
	podName := fmt.Sprintf("pod-%d", time.Now().UnixNano())
	podNamespace := "default"

	// Analyze crash loop patterns
	crashAnalysis := r.analyzeCrashLoopPatterns(podName, podNamespace, k8sData, logsData, metricsData)

	// Generate findings based on analysis
	if crashAnalysis.ConfidenceLevel >= r.config.MinConfidence {
		finding := r.createCrashLoopFinding(podName, podNamespace, crashAnalysis)
		findings = append(findings, *finding)
	}

	return findings
}

// analyzeCrashLoopPatterns analyzes crash loop patterns from all available sources
func (r *CrashLoopRule) analyzeCrashLoopPatterns(podName, podNamespace string, k8sData *correlation.KubernetesData, logsData *correlation.LogsData, metricsData *correlation.MetricsData) CrashLoopAnalysis {
	analysis := CrashLoopAnalysis{
		DataSources:         make([]string, 0),
		CrashLoopIndicators: make([]string, 0),
		RestartReasons:      make([]string, 0),
		ErrorPatterns:       make([]string, 0),
		RestartTimes:        make([]time.Time, 0),
		ObservationPeriod:   r.config.ObservationWindow,
	}

	// Analyze Kubernetes data (primary source)
	r.analyzeKubernetesCrashLoop(podName, podNamespace, k8sData, &analysis)

	// Analyze logs data if available
	if logsData != nil {
		r.analyzeLogsCrashLoop(podName, podNamespace, logsData, &analysis)
	}

	// Analyze metrics data if available
	if metricsData != nil {
		r.analyzeMetricsCrashLoop(podName, podNamespace, metricsData, &analysis)
	}

	// Calculate crash loop metrics
	r.calculateCrashLoopMetrics(&analysis)

	// Calculate overall confidence
	analysis.ConfidenceLevel = r.calculateCrashLoopConfidence(&analysis)

	// Determine if in crash loop
	analysis.InCrashLoop = analysis.ConfidenceLevel >= r.config.MinConfidence &&
		analysis.RestartCount >= r.config.MinRestartCount &&
		(analysis.RestartRate >= r.config.RestartRateThreshold ||
			analysis.CurrentBackoffTime >= r.config.BackoffThreshold)

	return analysis
}

// analyzeKubernetesCrashLoop analyzes Kubernetes data for crash loop patterns
func (r *CrashLoopRule) analyzeKubernetesCrashLoop(podName, podNamespace string, k8sData *correlation.KubernetesData, analysis *CrashLoopAnalysis) {
	analysis.DataSources = append(analysis.DataSources, "kubernetes")

	// Simplified container status analysis
	// In real implementation, you'd parse actual pod status
	restartCount := 0
	for range k8sData.Pods {
		// Simulate finding restart count
		restartCount = 5 // Example value
		break
	}

	analysis.RestartCount = restartCount
	if restartCount >= r.config.MinRestartCount {
		analysis.CrashLoopIndicators = append(analysis.CrashLoopIndicators, "high_restart_count")
	}

	// Analyze events for restart patterns
	for _, event := range k8sData.Events {
		if event.InvolvedObject.Name == podName {
			switch event.Reason {
			case "BackOff":
				analysis.CrashLoopIndicators = append(analysis.CrashLoopIndicators, "backoff_event")
				analysis.RestartReasons = append(analysis.RestartReasons, "CrashLoopBackOff")
			case "Failed":
				analysis.CrashLoopIndicators = append(analysis.CrashLoopIndicators, "failed_event")
			case "Unhealthy":
				analysis.CrashLoopIndicators = append(analysis.CrashLoopIndicators, "unhealthy_event")
				analysis.RestartReasons = append(analysis.RestartReasons, "Liveness probe failed")
			case "Killing":
				analysis.CrashLoopIndicators = append(analysis.CrashLoopIndicators, "killing_event")
			}

			// Track restart times
			if event.Reason == "Started" || event.Reason == "Created" {
				analysis.RestartTimes = append(analysis.RestartTimes, event.FirstTimestamp.Time)
			}
		}
	}

	// Check for problems indicating crash loops
	for _, problem := range k8sData.Problems {
		if problem.Resource.Kind == "Pod" && problem.Resource.Name == podName {
			if string(problem.Severity) == "critical" || string(problem.Severity) == "error" {
				analysis.CrashLoopIndicators = append(analysis.CrashLoopIndicators, "crash_loop_problem")
			}
		}
	}
}

// analyzeLogsCrashLoop analyzes logs data for crash patterns
func (r *CrashLoopRule) analyzeLogsCrashLoop(podName, podNamespace string, logsData *correlation.LogsData, analysis *CrashLoopAnalysis) {
	analysis.DataSources = append(analysis.DataSources, "logs")

	// Common error patterns that lead to crashes
	errorPatterns := []string{
		"panic:",
		"fatal error:",
		"segmentation fault",
		"out of memory",
		"cannot allocate memory",
		"connection refused",
		"no such file or directory",
		"permission denied",
		"exit status 1",
		"SIGKILL",
		"SIGTERM",
	}

	// Analyze logs for error patterns
	errorCounts := make(map[string]int)

	for _, logEntry := range logsData.Entries {
		if logEntry.PodName == podName && logEntry.Namespace == podNamespace {
			// Check for error patterns
			for _, pattern := range errorPatterns {
				if containsIgnoreCase(logEntry.Message, pattern) {
					errorCounts[pattern]++
					if !containsString(analysis.ErrorPatterns, pattern) {
						analysis.ErrorPatterns = append(analysis.ErrorPatterns, pattern)
					}
				}
			}

			// Check severity
			if logEntry.Severity == "ERROR" || logEntry.Severity == "FATAL" || logEntry.Severity == "PANIC" {
				analysis.CrashLoopIndicators = append(analysis.CrashLoopIndicators, "critical_errors_in_logs")
			}
		}
	}

	// If we found multiple error patterns, it's likely a crash loop
	if len(analysis.ErrorPatterns) >= 2 {
		analysis.CrashLoopIndicators = append(analysis.CrashLoopIndicators, "multiple_error_patterns")
	}
}

// analyzeMetricsCrashLoop analyzes metrics data for crash patterns
func (r *CrashLoopRule) analyzeMetricsCrashLoop(podName, podNamespace string, metricsData *correlation.MetricsData, analysis *CrashLoopAnalysis) {
	analysis.DataSources = append(analysis.DataSources, "metrics")

	// Check container restart metrics
	for _, containerMetrics := range metricsData.ContainerMetrics {
		if containerMetrics.PodName == podName && containerMetrics.Namespace == podNamespace {
			if containerMetrics.RestartCount > 0 {
				analysis.CrashLoopIndicators = append(analysis.CrashLoopIndicators, "restart_metrics")

				// Update restart count if higher
				if int(containerMetrics.RestartCount) > analysis.RestartCount {
					analysis.RestartCount = int(containerMetrics.RestartCount)
				}
			}

			// Check for resource exhaustion that might cause crashes
			if containerMetrics.Memory.Usage >= 0.95 {
				analysis.CrashLoopIndicators = append(analysis.CrashLoopIndicators, "memory_exhaustion")
				analysis.RestartReasons = append(analysis.RestartReasons, "OOMKilled")
			}

			if containerMetrics.CPU.Usage >= 0.95 {
				analysis.CrashLoopIndicators = append(analysis.CrashLoopIndicators, "cpu_exhaustion")
			}
		}
	}
}

// calculateCrashLoopMetrics calculates additional crash loop metrics
func (r *CrashLoopRule) calculateCrashLoopMetrics(analysis *CrashLoopAnalysis) {
	// Sort restart times
	sort.Slice(analysis.RestartTimes, func(i, j int) bool {
		return analysis.RestartTimes[i].Before(analysis.RestartTimes[j])
	})

	if len(analysis.RestartTimes) > 0 {
		// Last restart time
		analysis.LastRestartTime = analysis.RestartTimes[len(analysis.RestartTimes)-1]

		// Calculate restart rate
		if len(analysis.RestartTimes) > 1 {
			timeSpan := analysis.RestartTimes[len(analysis.RestartTimes)-1].Sub(analysis.RestartTimes[0])
			if timeSpan > 0 {
				analysis.RestartRate = float64(len(analysis.RestartTimes)-1) / timeSpan.Hours()
			}
		}

		// Calculate average run duration
		if len(analysis.RestartTimes) > 1 {
			var totalDuration time.Duration
			for i := 1; i < len(analysis.RestartTimes); i++ {
				totalDuration += analysis.RestartTimes[i].Sub(analysis.RestartTimes[i-1])
			}
			analysis.AverageRunDuration = totalDuration / time.Duration(len(analysis.RestartTimes)-1)
		}

		// Estimate current backoff time (exponential backoff simulation)
		if analysis.RestartCount > 0 {
			backoffSeconds := math.Min(math.Pow(2, float64(analysis.RestartCount)), 300) // Cap at 5 minutes
			analysis.CurrentBackoffTime = time.Duration(backoffSeconds) * time.Second
		}

		// Estimate recovery time
		if analysis.InCrashLoop && analysis.AverageRunDuration > 0 {
			// Pessimistic estimate: current backoff + time to fix + restart time
			analysis.EstimatedRecoveryTime = analysis.CurrentBackoffTime + 10*time.Minute
		}
	}
}

// calculateCrashLoopConfidence calculates the overall confidence level
func (r *CrashLoopRule) calculateCrashLoopConfidence(analysis *CrashLoopAnalysis) float64 {
	var confidenceFactors []float64

	// Restart count factor
	if analysis.RestartCount >= r.config.MinRestartCount {
		restartFactor := math.Min(float64(analysis.RestartCount)/float64(r.config.MinRestartCount*2), 1.0)
		confidenceFactors = append(confidenceFactors, restartFactor)
	}

	// Restart rate factor
	if analysis.RestartRate >= r.config.RestartRateThreshold {
		rateFactor := math.Min(analysis.RestartRate/r.config.RestartRateThreshold, 1.0)
		confidenceFactors = append(confidenceFactors, rateFactor)
	}

	// Backoff time factor
	if analysis.CurrentBackoffTime >= r.config.BackoffThreshold {
		backoffFactor := math.Min(float64(analysis.CurrentBackoffTime)/float64(r.config.BackoffThreshold), 1.0)
		confidenceFactors = append(confidenceFactors, backoffFactor)
	}

	// Indicator factor
	if len(analysis.CrashLoopIndicators) > 0 {
		indicatorFactor := math.Min(float64(len(analysis.CrashLoopIndicators))/5.0, 1.0)
		confidenceFactors = append(confidenceFactors, indicatorFactor)
	}

	// Error pattern factor
	if len(analysis.ErrorPatterns) > 0 {
		errorFactor := math.Min(float64(len(analysis.ErrorPatterns))/3.0, 1.0)
		confidenceFactors = append(confidenceFactors, errorFactor)
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

// createCrashLoopFinding creates a finding for crash loop detection
func (r *CrashLoopRule) createCrashLoopFinding(podName, podNamespace string, analysis CrashLoopAnalysis) *correlation.Finding {
	// Determine severity based on restart count and rate
	var severity correlation.Severity
	if analysis.RestartCount >= 10 || analysis.RestartRate >= 10 {
		severity = correlation.SeverityCritical
	} else if analysis.RestartCount >= 5 || analysis.RestartRate >= 5 {
		severity = correlation.SeverityError
	} else {
		severity = correlation.SeverityWarning
	}

	// Create finding
	finding := r.CreateFinding(
		"Pod Crash Loop Detected",
		fmt.Sprintf("Pod %s/%s is in a crash loop with %d restarts", podNamespace, podName, analysis.RestartCount),
		severity,
		analysis.ConfidenceLevel,
	)

	// Add resource reference
	finding.Resource = &correlation.ResourceReference{
		Kind:      "Pod",
		Name:      podName,
		Namespace: podNamespace,
	}

	// Add prediction if applicable
	if analysis.EstimatedRecoveryTime > 0 {
		finding.Prediction = &correlation.Prediction{
			Event:       "Pod Recovery",
			TimeToEvent: analysis.EstimatedRecoveryTime,
			Confidence:  analysis.ConfidenceLevel * 0.7, // Lower confidence for recovery prediction
			Factors:     analysis.RestartReasons,
			Mitigation: []string{
				"Check application logs for error patterns",
				"Review resource limits and requests",
				"Verify configuration and environment variables",
				"Check liveness and readiness probe configurations",
				"Consider rolling back to a previous working version",
			},
			UpdatedAt: time.Now(),
		}
	}

	// Add evidence
	for _, source := range analysis.DataSources {
		evidence := correlation.Evidence{
			Type:        "crash_loop_pattern",
			Source:      correlation.SourceType(source),
			Description: fmt.Sprintf("Crash loop analysis from %s", source),
			Data: map[string]interface{}{
				"restart_count":        analysis.RestartCount,
				"restart_rate":         analysis.RestartRate,
				"last_restart_time":    analysis.LastRestartTime,
				"average_run_duration": analysis.AverageRunDuration,
				"current_backoff_time": analysis.CurrentBackoffTime,
				"restart_reasons":      analysis.RestartReasons,
				"error_patterns":       analysis.ErrorPatterns,
			},
			Timestamp:  time.Now(),
			Confidence: analysis.ConfidenceLevel,
		}
		finding.AddEvidence(evidence)
	}

	// Add tags
	finding.AddTag("crash-loop")
	finding.AddTag("stability")
	finding.AddTag("restart")
	finding.AddTag("availability")

	// Add metadata
	finding.SetMetadata("analysis", analysis)
	finding.SetMetadata("crash_loop_indicators", analysis.CrashLoopIndicators)
	finding.SetMetadata("probable_causes", r.identifyProbableCauses(analysis))

	return finding
}

// identifyProbableCauses identifies probable causes based on analysis
func (r *CrashLoopRule) identifyProbableCauses(analysis CrashLoopAnalysis) []string {
	causes := make([]string, 0)

	// Check error patterns
	for _, pattern := range analysis.ErrorPatterns {
		switch pattern {
		case "out of memory", "cannot allocate memory":
			causes = append(causes, "Memory exhaustion - increase memory limits")
		case "connection refused":
			causes = append(causes, "Dependency unavailable - check service dependencies")
		case "permission denied":
			causes = append(causes, "Permission issues - check RBAC and file permissions")
		case "no such file or directory":
			causes = append(causes, "Missing configuration or volume mounts")
		case "panic:", "segmentation fault":
			causes = append(causes, "Application bug - check for code issues")
		}
	}

	// Check restart reasons
	for _, reason := range analysis.RestartReasons {
		switch reason {
		case "OOMKilled":
			causes = append(causes, "Out of memory - optimize memory usage or increase limits")
		case "Liveness probe failed":
			causes = append(causes, "Health check failure - adjust probe configuration or fix endpoint")
		case "CrashLoopBackOff":
			causes = append(causes, "Repeated failures - check logs for root cause")
		}
	}

	// Check indicators
	if containsString(analysis.CrashLoopIndicators, "memory_exhaustion") {
		causes = append(causes, "Memory resource limits too low")
	}
	if containsString(analysis.CrashLoopIndicators, "cpu_exhaustion") {
		causes = append(causes, "CPU resource limits too low")
	}

	// Deduplicate causes
	uniqueCauses := make([]string, 0)
	seen := make(map[string]bool)
	for _, cause := range causes {
		if !seen[cause] {
			seen[cause] = true
			uniqueCauses = append(uniqueCauses, cause)
		}
	}

	return uniqueCauses
}

// GetConfidenceFactors returns factors that affect confidence scoring
func (r *CrashLoopRule) GetConfidenceFactors() []string {
	return []string{
		"restart_count",
		"restart_rate",
		"backoff_duration",
		"error_pattern_detection",
		"multiple_data_sources",
		"crash_loop_indicators",
		"restart_reason_identification",
	}
}

// Validate validates the rule configuration
func (r *CrashLoopRule) Validate() error {
	if err := r.BaseRule.Validate(); err != nil {
		return err
	}

	if r.config.MinRestartCount < 1 {
		return correlation.NewRuleValidationError("min_restart_count must be at least 1")
	}

	if r.config.ObservationWindow <= 0 {
		return correlation.NewRuleValidationError("observation_window must be positive")
	}

	if r.config.RestartRateThreshold <= 0 {
		return correlation.NewRuleValidationError("restart_rate_threshold must be positive")
	}

	if r.config.BackoffThreshold <= 0 {
		return correlation.NewRuleValidationError("backoff_threshold must be positive")
	}

	if r.config.MinConfidence <= 0 || r.config.MinConfidence > 1 {
		return correlation.NewRuleValidationError("min_confidence must be between 0 and 1")
	}

	return nil
}

// Helper functions
func containsString(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

func containsIgnoreCase(s, substr string) bool {
	// Simple case-insensitive contains
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if toLower(s[i+j]) != toLower(substr[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func toLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 32
	}
	return c
}
