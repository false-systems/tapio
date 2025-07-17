package rules
import (
	"context"
	"fmt"
	"strings"
	"time"
	"github.com/falseyair/tapio/pkg/intelligence/correlation"
	"github.com/falseyair/tapio/pkg/domain"
)
// ControlPlaneDepsRule detects control plane dependency failures that cascade through components
// Pattern: Cloud provider timeout → controller-manager failures → scheduler issues → workload impacts
type ControlPlaneDepsRule struct {
	config ControlPlaneDepsConfig
}
// ControlPlaneDepsConfig configures the control plane dependency detection rule
type ControlPlaneDepsConfig struct {
	// Timeout threshold for external dependencies (seconds)
	TimeoutThreshold time.Duration
	// Minimum component failures to trigger
	MinComponentFailures int
	// Time window for correlation
	TimeWindow time.Duration
	// Minimum confidence required
	MinConfidence float64
	// Critical control plane components
	CriticalComponents []string
}
// DefaultControlPlaneDepsConfig returns the default configuration
func DefaultControlPlaneDepsConfig() ControlPlaneDepsConfig {
	return ControlPlaneDepsConfig{
		TimeoutThreshold:     30 * time.Second,
		MinComponentFailures: 2,
		TimeWindow:           10 * time.Minute,
		MinConfidence:        0.75,
		CriticalComponents: []string{
			"kube-controller-manager",
			"kube-scheduler",
			"cloud-controller-manager",
		},
	}
}
// NewControlPlaneDepsRule creates a new control plane dependency detection rule
func NewControlPlaneDepsRule(config ControlPlaneDepsConfig) *ControlPlaneDepsRule {
	return &ControlPlaneDepsRule{
		config: config,
	}
}
// ID returns the unique identifier for this rule
func (r *ControlPlaneDepsRule) ID() string {
	return "control_plane_dependency_failure"
}
// Name returns the human-readable name
func (r *ControlPlaneDepsRule) Name() string {
	return "Control Plane Dependency Failure Detection"
}
// Description returns a detailed description
func (r *ControlPlaneDepsRule) Description() string {
	return "Detects when external dependency failures cascade through control plane components causing cluster-wide disruption"
}
// GetMetadata returns metadata about the rule
func (r *ControlPlaneDepsRule) GetMetadata() correlation.RuleMetadata {
	return correlation.RuleMetadata{
		ID:          r.ID(),
		Name:        r.Name(),
		Description: r.Description(),
		Version:     "1.0.0",
		Author:      "Tapio Correlation Engine",
		Tags:        []string{"control-plane", "dependencies", "cascade", "infrastructure"},
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
func (r *ControlPlaneDepsRule) CheckRequirements(ctx context.Context, data *correlation.DataCollection) error {
	if !data.IsSourceAvailable(correlation.SourceKubernetes) {
		return correlation.NewRequirementNotMetError(r.ID(), r.GetMetadata().Requirements[0])
	}
	return nil
}
// GetConfidenceFactors returns factors that affect confidence scoring
func (r *ControlPlaneDepsRule) GetConfidenceFactors() []string {
	return []string{
		"dependency_timeout_correlation",
		"component_failure_count",
		"time_correlation",
		"cascade_pattern_match",
		"workload_impact_severity",
	}
}
// Validate validates the rule configuration
func (r *ControlPlaneDepsRule) Validate() error {
	if r.config.TimeoutThreshold <= 0 {
		return correlation.NewRuleValidationError("timeout_threshold must be positive")
	}
	if r.config.MinComponentFailures <= 0 {
		return correlation.NewRuleValidationError("min_component_failures must be positive")
	}
	if r.config.TimeWindow <= 0 {
		return correlation.NewRuleValidationError("time_window must be positive")
	}
	if r.config.MinConfidence <= 0 || r.config.MinConfidence > 1 {
		return correlation.NewRuleValidationError("min_confidence must be between 0 and 1")
	}
	return nil
}
// Execute runs the control plane dependency detection logic
func (r *ControlPlaneDepsRule) Execute(ctx context.Context, ruleCtx *correlation.RuleContext) ([]correlation.Finding, error) {
	var findings []correlation.Finding
	// Get Kubernetes data from rule context
	k8sData, err := ruleCtx.DataCollection.GetKubernetesData(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes data: %w", err)
	}
	// Get current time for analysis
	now := time.Now()
	windowStart := now.Add(-r.config.TimeWindow)
	// Check for external dependency issues
	depIssues := r.checkExternalDependencies(k8sData, windowStart)
	if len(depIssues) == 0 {
		return findings, nil // No dependency issues detected
	}
	// Check control plane component health
	componentFailures := r.checkComponentHealth(k8sData, windowStart)
	// Check for cascading failures
	cascadeEffects := r.checkCascadeEffects(k8sData, windowStart, componentFailures)
	// Check workload impact
	workloadImpact := r.checkWorkloadImpact(k8sData, windowStart)
	// Analyze dependency chains
	depChains := r.analyzeDependencyChains(depIssues, componentFailures, cascadeEffects)
	// Determine if this is a dependency cascade
	if len(componentFailures) >= r.config.MinComponentFailures && len(depChains) > 0 {
		confidence := r.calculateConfidence(depIssues, componentFailures, cascadeEffects, workloadImpact)
		if confidence >= r.config.MinConfidence {
			finding := correlation.Finding{
				ID:          "", // Will be auto-generated
				RuleID:      r.ID(),
				Title:       "Control Plane Dependency Cascade Detected",
				Description: r.buildDescription(depIssues, componentFailures, cascadeEffects, workloadImpact, depChains),
				Severity:    r.determineSeverity(componentFailures, workloadImpact),
				Confidence:  confidence,
				Evidence:    r.collectEvidence(depIssues, componentFailures, cascadeEffects, workloadImpact),
				Tags:        []string{"control-plane", "dependencies", "cascade"},
				CreatedAt:   time.Now(),
				UpdatedAt:   time.Now(),
				Metadata:    make(map[string]interface{}),
				Impact:      r.assessImpact(componentFailures, workloadImpact),
				RootCause:   r.identifyRootCause(depIssues, depChains),
				Recommendations: []string{
					"Check cloud provider API connectivity and quotas",
					"Verify control plane component configurations",
					"Review cloud provider credentials and permissions",
					"Consider increasing timeout values for external calls",
					"Implement circuit breakers for external dependencies",
					"Add health checks and retries for critical paths",
					"Review and optimize API rate limits",
				},
				Prediction: &correlation.RulePrediction{
					Event:       "Complete control plane failure",
					TimeToEvent: r.predictTimeToFailure(componentFailures, cascadeEffects),
					Confidence:  confidence,
					Factors:     []string{"dependency_timeouts", "component_failures", "cascade_effects"},
					Mitigation:  []string{"Fix external dependencies", "Restart components", "Implement circuit breakers"},
					UpdatedAt:   time.Now(),
				},
			}
			// Add affected resources
			for _, failure := range componentFailures {
				finding.AffectedResources = append(finding.AffectedResources, correlation.ResourceReference{
					Kind:      "Pod",
					Name:      failure.ComponentName,
					Namespace: failure.Namespace,
				})
			}
			findings = append(findings, finding)
		}
	}
	return findings, nil
}
// checkExternalDependencies identifies external dependency issues
func (r *ControlPlaneDepsRule) checkExternalDependencies(k8sData *correlation.KubernetesData, windowStart time.Time) []dependencyIssue {
	var issues []dependencyIssue
	// Common external dependencies to check
	externalDeps := []string{
		"cloud provider",
		"aws", "gcp", "azure",
		"iam", "sts", "metadata",
		"storage", "compute",
		"network", "loadbalancer",
		"dns", "route53",
	}
	// Check controller manager logs for cloud provider issues
	for _, pod := range k8sData.Pods {
		podInfo := PodInfo{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Labels:    pod.Labels,
		}
		if isControllerManagerPod(podInfo) || isCloudControllerPod(podInfo) {
			if logs, ok := k8sData.Logs[pod.Name]; ok {
				for _, logEntry := range logs {
					lineLower := strings.ToLower(logEntry.Message)
					// Check for timeout/connection issues
					if strings.Contains(lineLower, "timeout") ||
						strings.Contains(lineLower, "connection refused") ||
						strings.Contains(lineLower, "connection reset") ||
						strings.Contains(lineLower, "deadline exceeded") {
						// Check if it's related to external dependency
						for _, dep := range externalDeps {
							if strings.Contains(lineLower, dep) {
								issues = append(issues, dependencyIssue{
									Component:      pod.Name,
									Namespace:      pod.Namespace,
									DependencyType: identifyDependencyType(logEntry.Message),
									DependencyName: dep,
									IssueType:      "timeout",
									Message:        logEntry.Message,
									Timestamp:      logEntry.Timestamp,
								})
								break
							}
						}
					}
					// Check for authentication/permission issues
					if strings.Contains(lineLower, "unauthorized") ||
						strings.Contains(lineLower, "forbidden") ||
						strings.Contains(lineLower, "access denied") ||
						strings.Contains(lineLower, "permission") {
						for _, dep := range externalDeps {
							if strings.Contains(lineLower, dep) {
								issues = append(issues, dependencyIssue{
									Component:      pod.Name,
									Namespace:      pod.Namespace,
									DependencyType: identifyDependencyType(logEntry.Message),
									DependencyName: dep,
									IssueType:      "auth",
									Message:        logEntry.Message,
									Timestamp:      logEntry.Timestamp,
								})
								break
							}
						}
					}
					// Check for API rate limiting
					if strings.Contains(lineLower, "rate limit") ||
						strings.Contains(lineLower, "throttl") ||
						strings.Contains(lineLower, "too many requests") {
						issues = append(issues, dependencyIssue{
							Component:      pod.Name,
							Namespace:      pod.Namespace,
							DependencyType: "api",
							DependencyName: extractAPIFromMessage(logEntry.Message),
							IssueType:      "rate-limit",
							Message:        logEntry.Message,
							Timestamp:      logEntry.Timestamp,
						})
					}
				}
			}
		}
	}
	// Check events for dependency-related issues
	for _, event := range k8sData.Events {
		if event.CreationTimestamp.Time.After(windowStart) {
			eventLower := strings.ToLower(event.Message)
			for _, dep := range externalDeps {
				if strings.Contains(eventLower, dep) &&
					(strings.Contains(eventLower, "failed") ||
						strings.Contains(eventLower, "error") ||
						strings.Contains(eventLower, "timeout")) {
					issues = append(issues, dependencyIssue{
						Component:      event.InvolvedObject.Name,
						Namespace:      event.InvolvedObject.Namespace,
						DependencyType: identifyDependencyType(event.Message),
						DependencyName: dep,
						IssueType:      "error",
						Message:        event.Message,
						Timestamp:      event.CreationTimestamp.Time,
					})
				}
			}
		}
	}
	return issues
}
// checkComponentHealth checks control plane component health
func (r *ControlPlaneDepsRule) checkComponentHealth(k8sData *correlation.KubernetesData, windowStart time.Time) []componentFailure {
	var failures []componentFailure
	// Check each critical component
	for _, pod := range k8sData.Pods {
		podInfo := PodInfo{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			Labels:    pod.Labels,
		}
		componentType := identifyControlPlaneComponent(podInfo)
		if componentType == "" {
			continue
		}
		// Check if component is in critical list
		isCritical := false
		for _, critical := range r.config.CriticalComponents {
			if strings.Contains(componentType, critical) {
				isCritical = true
				break
			}
		}
		// Check pod status
		failure := componentFailure{
			ComponentName: pod.Name,
			ComponentType: componentType,
			Namespace:     pod.Namespace,
			IsCritical:    isCritical,
		}
		// Check container statuses
		for _, status := range pod.Status.ContainerStatuses {
			if !status.Ready {
				failure.Ready = false
				failure.RestartCount = int(status.RestartCount)
				if status.State.Waiting != nil {
					failure.State = "waiting"
					failure.Reason = status.State.Waiting.Reason
				} else if status.State.Terminated != nil {
					failure.State = "terminated"
					failure.Reason = status.State.Terminated.Reason
				}
			}
			// High restart count indicates issues
			if status.RestartCount > 3 {
				failure.HighRestarts = true
			}
		}
		// Check for error logs indicating dependency issues
		if logs, ok := k8sData.Logs[pod.Name]; ok {
			for _, logEntry := range logs {
				lineLower := strings.ToLower(logEntry.Message)
				// Connection/timeout errors
				if strings.Contains(lineLower, "connection") ||
					strings.Contains(lineLower, "timeout") ||
					strings.Contains(lineLower, "deadline") {
					failure.HasDependencyErrors = true
					failure.ErrorMessages = append(failure.ErrorMessages, logEntry.Message)
				}
				// Leader election issues
				if strings.Contains(lineLower, "leader election") &&
					strings.Contains(lineLower, "failed") {
					failure.LeaderElectionIssue = true
				}
			}
		}
		// Add if component has issues
		if !failure.Ready || failure.HighRestarts || failure.HasDependencyErrors {
			failures = append(failures, failure)
		}
	}
	return failures
}
// checkCascadeEffects identifies cascading effects through the system
func (r *ControlPlaneDepsRule) checkCascadeEffects(k8sData *correlation.KubernetesData, windowStart time.Time, componentFailures []componentFailure) []cascadeEffect {
	var effects []cascadeEffect
	// Map component types to their downstream effects
	componentEffects := map[string][]string{
		"controller-manager": {"deployment", "replicaset", "job", "service"},
		"scheduler":          {"pod", "node-binding"},
		"cloud-controller":   {"loadbalancer", "node", "route"},
	}
	// Check for cascade patterns
	for _, failure := range componentFailures {
		// Get expected effects for this component
		expectedEffects, ok := componentEffects[failure.ComponentType]
		if !ok {
			continue
		}
		effect := cascadeEffect{
			SourceComponent: failure.ComponentName,
			ComponentType:   failure.ComponentType,
			Timestamp:       time.Now(),
			AffectedObjects: make(map[string]int),
		}
		// Look for related failures in events
		for _, event := range k8sData.Events {
			if event.CreationTimestamp.Time.After(windowStart) {
				eventLower := strings.ToLower(event.Message)
				// Check if event relates to expected effects
				for _, expected := range expectedEffects {
					if strings.Contains(eventLower, expected) &&
						(strings.Contains(eventLower, "failed") ||
							strings.Contains(eventLower, "error") ||
							strings.Contains(eventLower, "timeout")) {
						effect.AffectedObjects[event.InvolvedObject.Kind]++
						effect.DownstreamFailures = append(effect.DownstreamFailures, downstreamFailure{
							ObjectKind: event.InvolvedObject.Kind,
							ObjectName: event.InvolvedObject.Name,
							Namespace:  event.InvolvedObject.Namespace,
							Error:      event.Message,
							Timestamp:  event.CreationTimestamp.Time,
						})
					}
				}
			}
		}
		// Check for stuck resources
		if failure.ComponentType == "controller-manager" {
			// Check deployments
			for _, deployment := range k8sData.Deployments {
				if deployment.Status.Replicas != deployment.Status.ReadyReplicas {
					effect.StuckResources++
				}
			}
		} else if failure.ComponentType == "scheduler" {
			// Check pending pods
			for _, pod := range k8sData.Pods {
				if pod.Status.Phase == "Pending" && pod.Spec.NodeName == "" {
					effect.StuckResources++
				}
			}
		}
		if len(effect.DownstreamFailures) > 0 || effect.StuckResources > 0 {
			effects = append(effects, effect)
		}
	}
	return effects
}
// checkWorkloadImpact assesses impact on workloads
func (r *ControlPlaneDepsRule) checkWorkloadImpact(k8sData *correlation.KubernetesData, windowStart time.Time) workloadImpact {
	impact := workloadImpact{
		AffectedNamespaces: make(map[string]int),
		AffectedWorkloads:  make(map[string]int),
	}
	// Check deployments
	for _, deployment := range k8sData.Deployments {
		if deployment.Status.Replicas != deployment.Status.ReadyReplicas {
			impact.AffectedWorkloads["Deployment"]++
			impact.AffectedNamespaces[deployment.Namespace]++
			impact.UnhealthyDeployments++
		}
		// Check for update issues
		if deployment.Status.UpdatedReplicas < deployment.Status.Replicas {
			impact.StuckUpdates++
		}
	}
	// Check statefulsets
	for _, sts := range k8sData.StatefulSets {
		if sts.Status.Replicas != sts.Status.ReadyReplicas {
			impact.AffectedWorkloads["StatefulSet"]++
			impact.AffectedNamespaces[sts.Namespace]++
			impact.UnhealthyStatefulSets++
		}
	}
	// Check daemonsets
	for _, ds := range k8sData.DaemonSets {
		if ds.Status.DesiredNumberScheduled != ds.Status.NumberReady {
			impact.AffectedWorkloads["DaemonSet"]++
			impact.AffectedNamespaces[ds.Namespace]++
			impact.UnhealthyDaemonSets++
		}
	}
	// Check pods
	for _, pod := range k8sData.Pods {
		switch pod.Status.Phase {
		case "Pending":
			impact.PendingPods++
			// Check if pending due to scheduling
			if pod.Spec.NodeName == "" {
				for _, condition := range pod.Status.Conditions {
					if condition.Type == "PodScheduled" && condition.Status == "False" {
						impact.UnscheduledPods++
						break
					}
				}
			}
		case "Failed", "Unknown":
			impact.FailedPods++
		}
	}
	// Check services
	for _, service := range k8sData.Services {
		if service.Spec.Type == "LoadBalancer" {
			// Check if LoadBalancer has ingress
			if len(service.Status.LoadBalancer.Ingress) == 0 {
				// Check how long it's been waiting
				for _, event := range k8sData.Events {
					if event.InvolvedObject.Kind == "Service" &&
						event.InvolvedObject.Name == service.Name &&
						strings.Contains(event.Message, "LoadBalancer") {
						impact.PendingLoadBalancers++
						break
					}
				}
			}
		}
	}
	return impact
}
// analyzeDependencyChains identifies dependency failure chains
func (r *ControlPlaneDepsRule) analyzeDependencyChains(depIssues []dependencyIssue, componentFailures []componentFailure, cascadeEffects []cascadeEffect) []dependencyChain {
	var chains []dependencyChain
	// Group dependency issues by type
	depByType := make(map[string][]dependencyIssue)
	for _, issue := range depIssues {
		depByType[issue.DependencyType] = append(depByType[issue.DependencyType], issue)
	}
	// Build chains for each dependency type
	for depType, issues := range depByType {
		chain := dependencyChain{
			RootDependency: depType,
			IssueCount:     len(issues),
		}
		// Find affected components
		for _, failure := range componentFailures {
			// Check if component failure relates to this dependency
			for _, issue := range issues {
				if issue.Component == failure.ComponentName {
					chain.AffectedComponents = append(chain.AffectedComponents, failure.ComponentType)
					break
				}
			}
		}
		// Find downstream effects
		for _, effect := range cascadeEffects {
			// Check if effect source is in affected components
			for _, comp := range chain.AffectedComponents {
				if strings.Contains(effect.ComponentType, comp) {
					chain.DownstreamImpacts = append(chain.DownstreamImpacts,
						fmt.Sprintf("%s affecting %d resources", effect.ComponentType, len(effect.DownstreamFailures)))
					break
				}
			}
		}
		if len(chain.AffectedComponents) > 0 {
			chains = append(chains, chain)
		}
	}
	return chains
}
// Helper methods
func (r *ControlPlaneDepsRule) calculateConfidence(depIssues []dependencyIssue, componentFailures []componentFailure, cascadeEffects []cascadeEffect, impact workloadImpact) float64 {
	confidence := 0.0
	// Dependency issues are the trigger
	if len(depIssues) > 0 {
		confidence = 0.3
		// Multiple dependency types increase confidence
		depTypes := make(map[string]bool)
		for _, issue := range depIssues {
			depTypes[issue.DependencyType] = true
		}
		if len(depTypes) > 1 {
			confidence += 0.1
		}
	}
	// Component failures confirm the pattern
	if len(componentFailures) > 0 {
		confidence += 0.2
		// Critical components have higher weight
		criticalCount := 0
		for _, failure := range componentFailures {
			if failure.IsCritical {
				criticalCount++
			}
		}
		if criticalCount > 1 {
			confidence += 0.1
		}
	}
	// Cascade effects show propagation
	if len(cascadeEffects) > 0 {
		confidence += 0.2
		// Many stuck resources indicate severe cascade
		totalStuck := 0
		for _, effect := range cascadeEffects {
			totalStuck += effect.StuckResources
		}
		if totalStuck > 10 {
			confidence += 0.1
		}
	}
	// Workload impact confirms severity
	if impact.UnhealthyDeployments > 5 || impact.PendingPods > 20 {
		confidence += 0.1
	}
	// Cap at 1.0
	if confidence > 1.0 {
		confidence = 1.0
	}
	return confidence
}
func (r *ControlPlaneDepsRule) buildDescription(depIssues []dependencyIssue, componentFailures []componentFailure, cascadeEffects []cascadeEffect, impact workloadImpact, chains []dependencyChain) string {
	parts := []string{
		"External dependency failures are cascading through control plane components.",
	}
	// Dependency issues summary
	if len(depIssues) > 0 {
		depTypes := make(map[string]int)
		for _, issue := range depIssues {
			depTypes[issue.DependencyType]++
		}
		parts = append(parts, fmt.Sprintf("\n\n🔗 Dependency Issues (%d detected):", len(depIssues)))
		for depType, count := range depTypes {
			parts = append(parts, fmt.Sprintf("  - %s: %d failures", depType, count))
		}
	}
	// Component failures
	if len(componentFailures) > 0 {
		parts = append(parts, fmt.Sprintf("\n\n⚠️  Component Failures (%d affected):", len(componentFailures)))
		for _, failure := range componentFailures {
			status := "degraded"
			if !failure.Ready {
				status = "not ready"
			} else if failure.HighRestarts {
				status = fmt.Sprintf("restarting (%d times)", failure.RestartCount)
			}
			parts = append(parts, fmt.Sprintf("  - %s: %s", failure.ComponentType, status))
		}
	}
	// Cascade effects
	if len(cascadeEffects) > 0 {
		parts = append(parts, "\n\n🌊 Cascade Effects:")
		for _, effect := range cascadeEffects {
			if len(effect.DownstreamFailures) > 0 {
				parts = append(parts, fmt.Sprintf("  - %s: %d downstream failures",
					effect.ComponentType, len(effect.DownstreamFailures)))
			}
			if effect.StuckResources > 0 {
				parts = append(parts, fmt.Sprintf("  - %s: %d resources stuck",
					effect.ComponentType, effect.StuckResources))
			}
		}
	}
	// Dependency chains
	if len(chains) > 0 {
		parts = append(parts, "\n\n⛓️  Dependency Chains:")
		for _, chain := range chains {
			parts = append(parts, fmt.Sprintf("  - %s → %s → workload impacts",
				chain.RootDependency, strings.Join(chain.AffectedComponents, ", ")))
		}
	}
	// Workload impact
	if impact.UnhealthyDeployments > 0 || impact.PendingPods > 0 {
		parts = append(parts, "\n\n📊 Workload Impact:")
		if impact.UnhealthyDeployments > 0 {
			parts = append(parts, fmt.Sprintf("  - %d unhealthy deployments", impact.UnhealthyDeployments))
		}
		if impact.PendingPods > 0 {
			parts = append(parts, fmt.Sprintf("  - %d pods pending", impact.PendingPods))
		}
		if impact.UnscheduledPods > 0 {
			parts = append(parts, fmt.Sprintf("  - %d pods cannot be scheduled", impact.UnscheduledPods))
		}
		if impact.PendingLoadBalancers > 0 {
			parts = append(parts, fmt.Sprintf("  - %d load balancers pending", impact.PendingLoadBalancers))
		}
	}
	parts = append(parts, "\n\n⚡ Pattern: External API failure → Control plane timeout → Component failure → Workload disruption")
	return strings.Join(parts, "\n")
}
func (r *ControlPlaneDepsRule) determineSeverity(componentFailures []componentFailure, impact workloadImpact) correlation.Severity {
	// Multiple critical components down
	criticalDown := 0
	for _, failure := range componentFailures {
		if failure.IsCritical && !failure.Ready {
			criticalDown++
		}
	}
	if criticalDown > 1 {
		return correlation.SeverityLevelCritical
	}
	// Significant workload impact
	if impact.UnhealthyDeployments > 10 || impact.PendingPods > 50 {
		return correlation.SeverityLevelCritical
	}
	// Leader election issues are critical
	for _, failure := range componentFailures {
		if failure.LeaderElectionIssue {
			return correlation.SeverityLevelCritical
		}
	}
	return correlation.SeverityLevelError
}
func (r *ControlPlaneDepsRule) assessImpact(componentFailures []componentFailure, impact workloadImpact) string {
	// Check for complete control plane failure
	criticalDown := 0
	for _, failure := range componentFailures {
		if failure.IsCritical && !failure.Ready {
			criticalDown++
		}
	}
	if criticalDown >= len(r.config.CriticalComponents) {
		return "Critical: Complete control plane failure, cluster operations halted"
	} else if impact.UnhealthyDeployments > 20 && impact.PendingPods > 50 {
		return "Severe: Widespread workload failures, new deployments impossible"
	} else if criticalDown > 0 {
		return "High: Critical control plane components failing, cluster stability at risk"
	}
	return "Moderate: Control plane degraded, some operations impaired"
}
func (r *ControlPlaneDepsRule) identifyRootCause(depIssues []dependencyIssue, chains []dependencyChain) string {
	// Count issue types
	issueTypes := make(map[string]int)
	for _, issue := range depIssues {
		issueTypes[issue.IssueType]++
	}
	// Identify primary issue type
	maxCount := 0
	primaryType := ""
	for issueType, count := range issueTypes {
		if count > maxCount {
			maxCount = count
			primaryType = issueType
		}
	}
	// Build root cause based on primary issue and dependency
	if len(chains) > 0 {
		primaryDep := chains[0].RootDependency
		switch primaryType {
		case "timeout":
			return fmt.Sprintf("%s API timeouts preventing control plane operations", primaryDep)
		case "auth":
			return fmt.Sprintf("%s authentication/authorization failures", primaryDep)
		case "rate-limit":
			return fmt.Sprintf("%s API rate limiting blocking control plane requests", primaryDep)
		default:
			return fmt.Sprintf("%s dependency failures cascading through control plane", primaryDep)
		}
	}
	return "External dependency failures affecting control plane components"
}
func (r *ControlPlaneDepsRule) collectEvidence(depIssues []dependencyIssue, componentFailures []componentFailure, cascadeEffects []cascadeEffect, impact workloadImpact) []correlation.RuleEvidence {
	evidence := []correlation.RuleEvidence{}
	now := time.Now()
	// Dependency evidence
	depTypes := make(map[string]int)
	for _, issue := range depIssues {
		depTypes[issue.DependencyType]++
	}
	for depType, count := range depTypes {
		evidence = append(evidence, correlation.RuleEvidence{
			Type:        "dependency_failure",
			Source:      correlation.SourceKubernetes,
			Description: fmt.Sprintf("%d %s dependency failures", count, depType),
			Data: map[string]interface{}{
				"dependency_type": depType,
				"failure_count":   count,
			},
			Timestamp:  now,
			Confidence: 0.9,
		})
	}
	// Component evidence
	if len(componentFailures) > 0 {
		evidence = append(evidence, correlation.RuleEvidence{
			Type:        "component_failure",
			Source:      correlation.SourceKubernetes,
			Description: fmt.Sprintf("%d control plane components affected", len(componentFailures)),
			Data: map[string]interface{}{
				"affected_components": len(componentFailures),
			},
			Timestamp:  now,
			Confidence: 0.95,
		})
		for _, failure := range componentFailures {
			if failure.LeaderElectionIssue {
				evidence = append(evidence, correlation.RuleEvidence{
					Type:        "leader_election_failure",
					Source:      correlation.SourceKubernetes,
					Description: fmt.Sprintf("%s: leader election failures", failure.ComponentType),
					Data: map[string]interface{}{
						"component_type": failure.ComponentType,
						"component_name": failure.ComponentName,
					},
					Timestamp:  now,
					Confidence: 1.0,
				})
			}
		}
	}
	// Cascade evidence
	totalDownstream := 0
	for _, effect := range cascadeEffects {
		totalDownstream += len(effect.DownstreamFailures)
	}
	if totalDownstream > 0 {
		evidence = append(evidence, correlation.RuleEvidence{
			Type:        "cascade_effect",
			Source:      correlation.SourceKubernetes,
			Description: fmt.Sprintf("%d downstream resource failures", totalDownstream),
			Data: map[string]interface{}{
				"downstream_failures": totalDownstream,
			},
			Timestamp:  now,
			Confidence: 0.85,
		})
	}
	// Impact evidence
	if impact.UnhealthyDeployments > 0 {
		evidence = append(evidence, correlation.RuleEvidence{
			Type:        "workload_impact",
			Source:      correlation.SourceKubernetes,
			Description: fmt.Sprintf("%d deployments unhealthy", impact.UnhealthyDeployments),
			Data: map[string]interface{}{
				"unhealthy_deployments": impact.UnhealthyDeployments,
			},
			Timestamp:  now,
			Confidence: 0.9,
		})
	}
	if impact.UnscheduledPods > 0 {
		evidence = append(evidence, correlation.RuleEvidence{
			Type:        "scheduling_failure",
			Source:      correlation.SourceKubernetes,
			Description: fmt.Sprintf("%d pods cannot be scheduled", impact.UnscheduledPods),
			Data: map[string]interface{}{
				"unscheduled_pods": impact.UnscheduledPods,
			},
			Timestamp:  now,
			Confidence: 0.95,
		})
	}
	return evidence
}
func (r *ControlPlaneDepsRule) predictTimeToFailure(componentFailures []componentFailure, cascadeEffects []cascadeEffect) time.Duration {
	// Check if already in failure state
	criticalDown := 0
	for _, failure := range componentFailures {
		if failure.IsCritical && !failure.Ready {
			criticalDown++
		}
	}
	if criticalDown >= len(r.config.CriticalComponents) {
		return 0 // Already failed
	}
	// Based on cascade progression
	if len(cascadeEffects) > 3 {
		return 5 * time.Minute
	} else if len(cascadeEffects) > 1 {
		return 15 * time.Minute
	} else if len(componentFailures) > 1 {
		return 30 * time.Minute
	}
	return 1 * time.Hour
}
// Helper functions
func isCloudControllerPod(pod PodInfo) bool {
	return strings.Contains(pod.Name, "cloud-controller-manager") ||
		(pod.Labels["component"] == "cloud-controller-manager")
}
func identifyControlPlaneComponent(pod PodInfo) string {
	// Check by pod name
	components := []string{
		"kube-controller-manager",
		"kube-scheduler",
		"cloud-controller-manager",
		"kube-apiserver",
		"etcd",
	}
	for _, comp := range components {
		if strings.Contains(pod.Name, comp) {
			// Normalize component name
			if strings.Contains(comp, "controller-manager") {
				return "controller-manager"
			} else if strings.Contains(comp, "scheduler") {
				return "scheduler"
			} else if strings.Contains(comp, "cloud-controller") {
				return "cloud-controller"
			}
			return comp
		}
	}
	// Check by label
	if component, ok := pod.Labels["component"]; ok {
		for _, comp := range components {
			if strings.Contains(component, comp) {
				return component
			}
		}
	}
	return ""
}
func identifyDependencyType(message string) string {
	msg := strings.ToLower(message)
	if strings.Contains(msg, "cloud") || strings.Contains(msg, "aws") ||
		strings.Contains(msg, "gcp") || strings.Contains(msg, "azure") {
		return "cloud-provider"
	} else if strings.Contains(msg, "storage") || strings.Contains(msg, "volume") {
		return "storage"
	} else if strings.Contains(msg, "network") || strings.Contains(msg, "loadbalancer") {
		return "network"
	} else if strings.Contains(msg, "iam") || strings.Contains(msg, "credential") {
		return "identity"
	} else if strings.Contains(msg, "dns") {
		return "dns"
	}
	return "external-api"
}
func extractAPIFromMessage(message string) string {
	// Try to extract API name from rate limit message
	patterns := []string{
		"api ", "service ", "endpoint ",
	}
	msg := strings.ToLower(message)
	for _, pattern := range patterns {
		if idx := strings.Index(msg, pattern); idx >= 0 {
			start := idx + len(pattern)
			parts := strings.Fields(message[start:])
			if len(parts) > 0 {
				return parts[0]
			}
		}
	}
	return "unknown"
}
// Helper types
type dependencyIssue struct {
	Component      string
	Namespace      string
	DependencyType string
	DependencyName string
	IssueType      string // "timeout", "auth", "rate-limit", "error"
	Message        string
	Timestamp      time.Time
}
type componentFailure struct {
	ComponentName       string
	ComponentType       string
	Namespace           string
	IsCritical          bool
	Ready               bool
	State               string
	Reason              string
	RestartCount        int
	HighRestarts        bool
	HasDependencyErrors bool
	LeaderElectionIssue bool
	ErrorMessages       []string
}
type cascadeEffect struct {
	SourceComponent    string
	ComponentType      string
	DownstreamFailures []downstreamFailure
	AffectedObjects    map[string]int
	StuckResources     int
	Timestamp          time.Time
}
type downstreamFailure struct {
	ObjectKind string
	ObjectName string
	Namespace  string
	Error      string
	Timestamp  time.Time
}
type workloadImpact struct {
	AffectedNamespaces    map[string]int
	AffectedWorkloads     map[string]int
	UnhealthyDeployments  int
	UnhealthyStatefulSets int
	UnhealthyDaemonSets   int
	PendingPods           int
	FailedPods            int
	UnscheduledPods       int
	StuckUpdates          int
	PendingLoadBalancers  int
}
type dependencyChain struct {
	RootDependency     string
	AffectedComponents []string
	DownstreamImpacts  []string
	IssueCount         int
}
