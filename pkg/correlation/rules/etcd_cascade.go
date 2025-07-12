package rules

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	"github.com/yairfalse/tapio/pkg/correlation"
	"github.com/yairfalse/tapio/pkg/types"
)

// ETCDCascadeRule detects etcd cascading failures that affect the entire control plane
// Pattern: etcd health → control plane → data plane isolation
type ETCDCascadeRule struct {
	config ETCDCascadeConfig
}

// ETCDCascadeConfig configures the etcd cascade detection rule
type ETCDCascadeConfig struct {
	// Memory threshold for etcd (percentage of limit)
	MemoryThreshold float64
	// API server timeout threshold (seconds)
	APITimeoutThreshold time.Duration
	// Minimum confidence required
	MinConfidence float64
	// Time window for correlation
	TimeWindow time.Duration
}

// DefaultETCDCascadeConfig returns the default configuration
func DefaultETCDCascadeConfig() ETCDCascadeConfig {
	return ETCDCascadeConfig{
		MemoryThreshold:     85.0, // 85% memory usage
		APITimeoutThreshold: 5 * time.Second,
		MinConfidence:       0.75,
		TimeWindow:          5 * time.Minute,
	}
}

// NewETCDCascadeRule creates a new etcd cascade detection rule
func NewETCDCascadeRule(config ETCDCascadeConfig) *ETCDCascadeRule {
	return &ETCDCascadeRule{
		config: config,
	}
}

// ID returns the unique identifier for this rule
func (r *ETCDCascadeRule) ID() string {
	return "etcd_cascade_failure"
}

// Name returns the human-readable name
func (r *ETCDCascadeRule) Name() string {
	return "ETCD Cascading Failure Detection"
}

// Description returns a detailed description
func (r *ETCDCascadeRule) Description() string {
	return "Detects when etcd memory pressure leads to API server timeouts and DNS initialization failures, causing cluster-wide disruption"
}

// GetMetadata returns metadata about the rule
func (r *ETCDCascadeRule) GetMetadata() correlation.RuleMetadata {
	return correlation.RuleMetadata{
		ID:          r.ID(),
		Name:        r.Name(),
		Description: r.Description(),
		Version:     "1.0.0",
		Author:      "Tapio Correlation Engine",
		Tags:        []string{"etcd", "cascade", "control-plane", "memory"},
		Requirements: []correlation.RuleRequirement{
			{
				SourceType: correlation.SourceKubernetes,
				DataType:   "full",
				Required:   true,
			},
		},
		Enabled:   true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

// CheckRequirements verifies that required data sources are available
func (r *ETCDCascadeRule) CheckRequirements(ctx context.Context, data *correlation.DataCollection) error {
	if !data.IsSourceAvailable(correlation.SourceKubernetes) {
		return correlation.NewRequirementNotMetError(r.ID(), r.GetMetadata().Requirements[0])
	}
	return nil
}

// GetConfidenceFactors returns factors that affect confidence scoring
func (r *ETCDCascadeRule) GetConfidenceFactors() []string {
	return []string{
		"etcd_memory_pressure",
		"api_server_timeout_correlation",
		"dns_failure_patterns",
		"control_plane_component_health",
		"cascade_timing_analysis",
	}
}

// Validate validates the rule configuration
func (r *ETCDCascadeRule) Validate() error {
	if r.config.MemoryThreshold <= 0 || r.config.MemoryThreshold > 100 {
		return correlation.NewRuleValidationError("memory_threshold must be between 0 and 100")
	}
	if r.config.APITimeoutThreshold <= 0 {
		return correlation.NewRuleValidationError("api_timeout_threshold must be positive")
	}
	if r.config.TimeWindow <= 0 {
		return correlation.NewRuleValidationError("time_window must be positive")
	}
	if r.config.MinConfidence <= 0 || r.config.MinConfidence > 1 {
		return correlation.NewRuleValidationError("min_confidence must be between 0 and 1")
	}
	return nil
}

// Execute runs the etcd cascade detection logic
func (r *ETCDCascadeRule) Execute(ctx context.Context, ruleCtx *correlation.RuleContext) ([]correlation.Finding, error) {
	var findings []correlation.Finding

	// Get Kubernetes data from rule context
	k8sData, err := ruleCtx.DataCollection.GetKubernetesData(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes data: %w", err)
	}

	// Get current time for time window analysis
	now := time.Now()
	windowStart := now.Add(-r.config.TimeWindow)

	// Check for etcd memory pressure
	etcdMemoryIssues := r.checkETCDMemory(k8sData, windowStart)
	if len(etcdMemoryIssues) == 0 {
		return findings, nil // No etcd memory issues, no cascade
	}

	// Check for API server timeouts
	apiTimeouts := r.checkAPIServerTimeouts(k8sData, windowStart)

	// Check for DNS initialization failures
	dnsFailures := r.checkDNSFailures(k8sData, windowStart)

	// Check for workload failures
	workloadFailures := r.checkWorkloadFailures(k8sData, windowStart)

	// Correlate findings
	if len(etcdMemoryIssues) > 0 && (len(apiTimeouts) > 0 || len(dnsFailures) > 0) {
		confidence := r.calculateConfidence(etcdMemoryIssues, apiTimeouts, dnsFailures, workloadFailures)

		if confidence >= r.config.MinConfidence {
			// Create comprehensive finding
			finding := correlation.Finding{
				ID:          "", // Will be auto-generated
				RuleID:      r.ID(),
				Title:       "ETCD Cascading Failure Detected",
				Description: r.buildDescription(etcdMemoryIssues, apiTimeouts, dnsFailures, workloadFailures),
				Severity:    correlation.SeverityCritical,
				Confidence:  confidence,
				Evidence:    r.collectEvidence(etcdMemoryIssues, apiTimeouts, dnsFailures, workloadFailures),
				Tags:        []string{"etcd", "cascade", "memory", "critical"},
				CreatedAt:   time.Now(),
				UpdatedAt:   time.Now(),
				Metadata:    make(map[string]interface{}),
				Impact:      r.assessImpact(apiTimeouts, dnsFailures, workloadFailures),
				RootCause:   r.identifyRootCause(etcdMemoryIssues),
				Recommendations: []string{
					"Immediately increase etcd memory limits",
					"Restart etcd pods in a rolling fashion",
					"Consider scaling etcd cluster horizontally",
					"Review etcd compaction and defragmentation settings",
					"Check for large keys or excessive watch operations",
				},
				Prediction: &correlation.Prediction{
					Event:       "Complete cluster control plane failure",
					TimeToEvent: r.predictTimeToFailure(etcdMemoryIssues, apiTimeouts),
					Confidence:  confidence,
					Factors:     []string{"etcd_memory_pressure", "api_timeouts", "dns_failures"},
					Mitigation:  []string{"Increase etcd memory", "Restart components", "Scale etcd cluster"},
					UpdatedAt:   time.Now(),
				},
			}

			// Add resource references
			for _, issue := range etcdMemoryIssues {
				finding.AffectedResources = append(finding.AffectedResources, correlation.ResourceReference{
					Kind:      "Pod",
					Name:      issue.PodName,
					Namespace: issue.Namespace,
				})
			}

			findings = append(findings, finding)
		}
	}

	return findings, nil
}

// checkETCDMemory checks for etcd memory pressure
func (r *ETCDCascadeRule) checkETCDMemory(k8sData *correlation.KubernetesData, windowStart time.Time) []etcdMemoryIssue {
	var issues []etcdMemoryIssue

	// Check Kubernetes data for etcd pods
	for _, pod := range k8sData.Pods {
		// Identify etcd pods by label or name
		if !isETCDPod(pod) {
			continue
		}

		// Check memory usage
		for _, container := range pod.Status.ContainerStatuses {
			if container.Name != "etcd" {
				continue
			}

			// Get memory metrics from container
			memoryUsage := getContainerMemoryUsage(pod, container.Name, k8sData)
			memoryLimit := getContainerMemoryLimit(pod, container.Name)

			if memoryLimit > 0 && memoryUsage > 0 {
				usagePercent := (float64(memoryUsage) / float64(memoryLimit)) * 100

				if usagePercent >= r.config.MemoryThreshold {
					issues = append(issues, etcdMemoryIssue{
						PodName:      pod.Name,
						Namespace:    pod.Namespace,
						MemoryUsage:  memoryUsage,
						MemoryLimit:  memoryLimit,
						UsagePercent: usagePercent,
						Timestamp:    time.Now(),
					})
				}
			}
		}
	}

	// Check eBPF data for more accurate memory stats
	if k8sData.EBPFData != nil {
		for _, memStat := range k8sData.EBPFData.MemoryStats {
			// Match process to etcd
			if strings.Contains(memStat.Command, "etcd") {
				// Convert to percentage if we have container info
				pod := findPodByPID(k8sData, memStat.PID)
				if pod != nil && isETCDPod(pod) {
					limit := getContainerMemoryLimit(pod, "etcd")
					if limit > 0 {
						usagePercent := (float64(memStat.CurrentUsage) / float64(limit)) * 100
						if usagePercent >= r.config.MemoryThreshold {
							issues = append(issues, etcdMemoryIssue{
								PodName:      pod.Name,
								Namespace:    pod.Namespace,
								MemoryUsage:  memStat.CurrentUsage,
								MemoryLimit:  limit,
								UsagePercent: usagePercent,
								Timestamp:    time.Now(),
								HasEBPFData:  true,
							})
						}
					}
				}
			}
		}
	}

	return issues
}

// checkAPIServerTimeouts checks for API server timeout issues
func (r *ETCDCascadeRule) checkAPIServerTimeouts(k8sData *correlation.KubernetesData, windowStart time.Time) []apiTimeoutIssue {
	var timeouts []apiTimeoutIssue

	// Check for API server pods with high latency or timeouts
	for _, pod := range k8sData.Pods {
		if !isAPIServerPod(pod) {
			continue
		}

		// Check pod events for timeout-related messages
		for _, event := range k8sData.Events {
			if event.InvolvedObject.Name == pod.Name &&
				event.CreationTimestamp.Time.After(windowStart) &&
				(strings.Contains(strings.ToLower(event.Message), "timeout") ||
					strings.Contains(strings.ToLower(event.Message), "etcd")) {

				timeouts = append(timeouts, apiTimeoutIssue{
					PodName:   pod.Name,
					Namespace: pod.Namespace,
					Message:   event.Message,
					Timestamp: event.CreationTimestamp.Time,
				})
			}
		}

		// Check container logs if available
		if logs, ok := k8sData.Logs[pod.Name]; ok {
			for _, logEntry := range logs {
				if strings.Contains(strings.ToLower(logEntry.Message), "etcd timeout") ||
					strings.Contains(strings.ToLower(logEntry.Message), "context deadline exceeded") {
					timeouts = append(timeouts, apiTimeoutIssue{
						PodName:   pod.Name,
						Namespace: pod.Namespace,
						Message:   logEntry.Message,
						Timestamp: logEntry.Timestamp,
					})
				}
			}
		}
	}

	return timeouts
}

// checkDNSFailures checks for DNS initialization failures
func (r *ETCDCascadeRule) checkDNSFailures(k8sData *correlation.KubernetesData, windowStart time.Time) []dnsFailure {
	var failures []dnsFailure

	// Check CoreDNS pods
	for _, pod := range k8sData.Pods {
		if !isCoreDNSPod(pod) {
			continue
		}

		// Check if pod is crash looping or failing
		for _, status := range pod.Status.ContainerStatuses {
			if status.RestartCount > 2 {
				failures = append(failures, dnsFailure{
					PodName:      pod.Name,
					Namespace:    pod.Namespace,
					RestartCount: int(status.RestartCount),
					Ready:        status.Ready,
					Timestamp:    time.Now(),
				})
			}
		}

		// Check events
		for _, event := range k8sData.Events {
			if event.InvolvedObject.Name == pod.Name &&
				event.CreationTimestamp.Time.After(windowStart) &&
				(strings.Contains(strings.ToLower(event.Message), "failed") ||
					strings.Contains(strings.ToLower(event.Message), "error")) {

				failures = append(failures, dnsFailure{
					PodName:   pod.Name,
					Namespace: pod.Namespace,
					Message:   event.Message,
					Timestamp: event.CreationTimestamp.Time,
				})
			}
		}
	}

	return failures
}

// checkWorkloadFailures checks for workload initialization failures
func (r *ETCDCascadeRule) checkWorkloadFailures(k8sData *correlation.KubernetesData, windowStart time.Time) []workloadFailure {
	var failures []workloadFailure

	// Check for pods stuck in init or crashing
	for _, pod := range k8sData.Pods {
		// Skip system pods
		if pod.Namespace == "kube-system" || pod.Namespace == "kube-public" {
			continue
		}

		// Check for init container failures
		for _, status := range pod.Status.InitContainerStatuses {
			if status.State.Waiting != nil &&
				(strings.Contains(strings.ToLower(status.State.Waiting.Reason), "dns") ||
					strings.Contains(strings.ToLower(status.State.Waiting.Message), "dns")) {

				failures = append(failures, workloadFailure{
					PodName:   pod.Name,
					Namespace: pod.Namespace,
					Phase:     string(pod.Status.Phase),
					Reason:    status.State.Waiting.Reason,
					Message:   status.State.Waiting.Message,
					Timestamp: time.Now(),
				})
			}
		}
	}

	return failures
}

// Helper functions

func (r *ETCDCascadeRule) calculateConfidence(etcd []etcdMemoryIssue, api []apiTimeoutIssue, dns []dnsFailure, workload []workloadFailure) float64 {
	confidence := 0.0

	// Base confidence from etcd memory pressure
	if len(etcd) > 0 {
		confidence = 0.4

		// Higher confidence if multiple etcd instances affected
		if len(etcd) > 1 {
			confidence += 0.1
		}

		// Add confidence for very high memory usage
		for _, issue := range etcd {
			if issue.UsagePercent > 95 {
				confidence += 0.1
				break
			}
		}
	}

	// API server timeouts strongly indicate cascade
	if len(api) > 0 {
		confidence += 0.3
		if len(api) > 2 {
			confidence += 0.1
		}
	}

	// DNS failures confirm cascade
	if len(dns) > 0 {
		confidence += 0.2
	}

	// Workload failures indicate full cascade
	if len(workload) > 0 {
		confidence += 0.1
		if len(workload) > 5 {
			confidence += 0.1
		}
	}

	// Cap at 1.0
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

func (r *ETCDCascadeRule) buildDescription(etcd []etcdMemoryIssue, api []apiTimeoutIssue, dns []dnsFailure, workload []workloadFailure) string {
	parts := []string{
		"Critical: ETCD memory pressure is causing a cascading failure across the control plane.",
	}

	if len(etcd) > 0 {
		parts = append(parts, fmt.Sprintf("\n\n🔴 ETCD Issues (%d instances):", len(etcd)))
		for _, issue := range etcd {
			parts = append(parts, fmt.Sprintf("  - %s: %.1f%% memory usage", issue.PodName, issue.UsagePercent))
		}
	}

	if len(api) > 0 {
		parts = append(parts, fmt.Sprintf("\n\n⚠️  API Server Impact (%d timeouts):", len(api)))
		if len(api) > 3 {
			parts = append(parts, "  - Multiple API servers experiencing etcd connection timeouts")
		} else {
			for _, timeout := range api[:min(3, len(api))] {
				parts = append(parts, fmt.Sprintf("  - %s: timeout errors", timeout.PodName))
			}
		}
	}

	if len(dns) > 0 {
		parts = append(parts, fmt.Sprintf("\n\n🌐 DNS System Affected (%d failures):", len(dns)))
		parts = append(parts, "  - CoreDNS pods failing to initialize or crashing")
	}

	if len(workload) > 0 {
		parts = append(parts, fmt.Sprintf("\n\n📦 Workload Impact (%d pods):", len(workload)))
		parts = append(parts, "  - Application pods unable to resolve DNS")
		parts = append(parts, "  - New deployments failing to start")
	}

	parts = append(parts, "\n\n⚡ Cascade Pattern: etcd OOM → API server timeouts → DNS failures → workload disruption")

	return strings.Join(parts, "\n")
}

func (r *ETCDCascadeRule) assessImpact(api []apiTimeoutIssue, dns []dnsFailure, workload []workloadFailure) string {
	if len(workload) > 10 {
		return "Cluster-wide outage: Control plane is non-functional, all workloads affected"
	} else if len(dns) > 0 && len(api) > 0 {
		return "Critical: Control plane degraded, new workloads cannot start"
	} else if len(api) > 0 {
		return "Severe: API server instability, cluster operations impaired"
	}
	return "High: ETCD performance degradation affecting cluster stability"
}

func (r *ETCDCascadeRule) identifyRootCause(etcd []etcdMemoryIssue) string {
	if len(etcd) == 0 {
		return "Unknown"
	}

	// Check if all etcd instances are affected
	if len(etcd) > 1 {
		return "ETCD cluster-wide memory exhaustion, possibly due to large keyspace or excessive watch operations"
	}

	// Check for very high usage
	maxUsage := 0.0
	for _, issue := range etcd {
		if issue.UsagePercent > maxUsage {
			maxUsage = issue.UsagePercent
		}
	}

	if maxUsage > 95 {
		return fmt.Sprintf("ETCD memory critically exhausted (%.1f%%), immediate action required", maxUsage)
	}

	return fmt.Sprintf("ETCD memory pressure (%.1f%%), trending toward exhaustion", maxUsage)
}

func (r *ETCDCascadeRule) collectEvidence(etcd []etcdMemoryIssue, api []apiTimeoutIssue, dns []dnsFailure, workload []workloadFailure) []correlation.Evidence {
	evidence := []correlation.Evidence{}

	for _, issue := range etcd {
		evidence = append(evidence, correlation.Evidence{
			Type:        "etcd_memory_pressure",
			Source:      correlation.SourceKubernetes,
			Description: fmt.Sprintf("ETCD pod %s at %.1f%% memory usage", issue.PodName, issue.UsagePercent),
			Data: map[string]interface{}{
				"pod_name":      issue.PodName,
				"usage_percent": issue.UsagePercent,
				"namespace":     issue.Namespace,
			},
			Timestamp:  issue.Timestamp,
			Confidence: 0.95,
		})
		if issue.HasEBPFData {
			evidence = append(evidence, correlation.Evidence{
				Type:        "kernel_memory_tracking",
				Source:      correlation.SourceEBPF,
				Description: "Kernel-level memory tracking confirms high usage",
				Data: map[string]interface{}{
					"pod_name": issue.PodName,
				},
				Timestamp:  issue.Timestamp,
				Confidence: 1.0,
			})
		}
	}

	if len(api) > 0 {
		evidence = append(evidence, correlation.Evidence{
			Type:        "api_timeout_errors",
			Source:      correlation.SourceKubernetes,
			Description: fmt.Sprintf("%d API server timeout errors detected", len(api)),
			Data: map[string]interface{}{
				"timeout_count": len(api),
			},
			Timestamp:  time.Now(),
			Confidence: 0.9,
		})
	}

	if len(dns) > 0 {
		evidence = append(evidence, correlation.Evidence{
			Type:        "dns_failures",
			Source:      correlation.SourceKubernetes,
			Description: "CoreDNS pods failing or restarting",
			Data: map[string]interface{}{
				"failure_count": len(dns),
			},
			Timestamp:  time.Now(),
			Confidence: 0.85,
		})
	}

	if len(workload) > 0 {
		evidence = append(evidence, correlation.Evidence{
			Type:        "workload_dns_impact",
			Source:      correlation.SourceKubernetes,
			Description: fmt.Sprintf("%d workload pods affected by DNS resolution failures", len(workload)),
			Data: map[string]interface{}{
				"affected_workloads": len(workload),
			},
			Timestamp:  time.Now(),
			Confidence: 0.8,
		})
	}

	return evidence
}

func (r *ETCDCascadeRule) predictTimeToFailure(etcd []etcdMemoryIssue, api []apiTimeoutIssue) time.Duration {
	// If API servers already timing out, failure is imminent
	if len(api) > 0 {
		return 5 * time.Minute
	}

	// Calculate based on memory pressure
	maxUsage := 0.0
	for _, issue := range etcd {
		if issue.UsagePercent > maxUsage {
			maxUsage = issue.UsagePercent
		}
	}

	// Exponential decrease in time as usage approaches 100%
	if maxUsage > 95 {
		return 10 * time.Minute
	} else if maxUsage > 90 {
		return 30 * time.Minute
	} else if maxUsage > 85 {
		return 1 * time.Hour
	}

	return 2 * time.Hour
}

// Helper types
type etcdMemoryIssue struct {
	PodName      string
	Namespace    string
	MemoryUsage  uint64
	MemoryLimit  uint64
	UsagePercent float64
	Timestamp    time.Time
	HasEBPFData  bool
}

type apiTimeoutIssue struct {
	PodName   string
	Namespace string
	Message   string
	Timestamp time.Time
}

type dnsFailure struct {
	PodName      string
	Namespace    string
	RestartCount int
	Ready        bool
	Message      string
	Timestamp    time.Time
}

type workloadFailure struct {
	PodName   string
	Namespace string
	Phase     string
	Reason    string
	Message   string
	Timestamp time.Time
}

// Utility functions
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
