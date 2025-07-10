package rules

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/falseyair/tapio/pkg/correlation"
)

// AdmissionLockdownRule detects when strict policies lock down the cluster preventing operations
// Pattern: Policy tightening → service account denials → controller failures → operational paralysis
type AdmissionLockdownRule struct {
	config AdmissionLockdownConfig
}

// AdmissionLockdownConfig configures the admission lockdown detection rule
type AdmissionLockdownConfig struct {
	// Minimum denials to trigger detection
	MinDenials int
	// Percentage of requests denied to consider critical
	CriticalDenialPercent float64
	// Time window for correlation
	TimeWindow time.Duration
	// Minimum confidence required
	MinConfidence float64
	// System namespaces to monitor closely
	SystemNamespaces []string
}

// DefaultAdmissionLockdownConfig returns the default configuration
func DefaultAdmissionLockdownConfig() AdmissionLockdownConfig {
	return AdmissionLockdownConfig{
		MinDenials:            10,
		CriticalDenialPercent: 30.0,
		TimeWindow:            15 * time.Minute,
		MinConfidence:         0.75,
		SystemNamespaces:      []string{"kube-system", "kube-public", "kube-node-lease"},
	}
}

// NewAdmissionLockdownRule creates a new admission lockdown detection rule
func NewAdmissionLockdownRule(config AdmissionLockdownConfig) *AdmissionLockdownRule {
	return &AdmissionLockdownRule{
		config: config,
	}
}

// ID returns the unique identifier for this rule
func (r *AdmissionLockdownRule) ID() string {
	return "admission_controller_lockdown"
}

// Name returns the human-readable name
func (r *AdmissionLockdownRule) Name() string {
	return "Admission Controller Lockdown Detection"
}

// Description returns a detailed description
func (r *AdmissionLockdownRule) Description() string {
	return "Detects when overly restrictive admission policies prevent legitimate operations, locking down cluster functionality"
}

// Execute runs the admission lockdown detection logic
func (r *AdmissionLockdownRule) Execute(ctx context.Context, data *correlation.AnalysisData) ([]correlation.Finding, error) {
	var findings []correlation.Finding

	// Get current time for analysis
	now := time.Now()
	windowStart := now.Add(-r.config.TimeWindow)

	// Analyze admission denials
	denials := r.analyzeAdmissionDenials(data, windowStart)
	if len(denials) < r.config.MinDenials {
		return findings, nil // Not enough denials to indicate lockdown
	}

	// Check service account denials
	sadenials := r.checkServiceAccountDenials(denials)

	// Check controller failures
	controllerFailures := r.checkControllerFailures(data, windowStart, denials)

	// Check operational impact
	operationalImpact := r.checkOperationalImpact(data, windowStart, denials)

	// Calculate denial patterns
	denialStats := r.calculateDenialStatistics(denials)

	// Determine if this is a lockdown scenario
	if denialStats.denialRate > r.config.CriticalDenialPercent || len(sadenials) > 5 {
		confidence := r.calculateConfidence(denialStats, sadenials, controllerFailures, operationalImpact)

		if confidence >= r.config.MinConfidence {
			finding := correlation.Finding{
				RuleID:      r.ID(),
				Title:       "Admission Controller Lockdown Detected",
				Description: r.buildDescription(denialStats, sadenials, controllerFailures, operationalImpact),
				Severity:    r.determineSeverity(denialStats, sadenials, controllerFailures),
				Confidence:  confidence,
				Impact:      r.assessImpact(denialStats, controllerFailures, operationalImpact),
				RootCause:   r.identifyRootCause(denials, sadenials),
				Evidence:    r.collectEvidence(denialStats, sadenials, controllerFailures, operationalImpact),
				Recommendations: []string{
					"Review recent admission policy changes",
					"Identify overly restrictive rules affecting service accounts",
					"Temporarily relax policies for critical operations",
					"Add exemptions for system service accounts",
					"Review RBAC permissions for affected controllers",
					"Consider implementing policy dry-run before enforcement",
					"Check for misconfigured policy engines (OPA, Kyverno, etc.)",
				},
				Prediction: &correlation.Prediction{
					Event:       "Complete operational paralysis",
					TimeToEvent: r.predictTimeToParalysis(denialStats, controllerFailures),
					Confidence:  confidence,
				},
			}

			// Add affected resources
			policyResources := r.identifyPolicyResources(denials)
			for _, resource := range policyResources {
				finding.AffectedResources = append(finding.AffectedResources, resource)
			}

			findings = append(findings, finding)
		}
	}

	return findings, nil
}

// analyzeAdmissionDenials collects and analyzes admission denial events
func (r *AdmissionLockdownRule) analyzeAdmissionDenials(data *correlation.AnalysisData, windowStart time.Time) []admissionDenial {
	var denials []admissionDenial

	// Look for admission denial events
	for _, event := range data.KubernetesData.Events {
		if event.CreatedAt.After(windowStart) &&
			event.Type == "Warning" &&
			(strings.Contains(strings.ToLower(event.Message), "denied") ||
				strings.Contains(strings.ToLower(event.Message), "forbidden") ||
				strings.Contains(strings.ToLower(event.Message), "rejected") ||
				strings.Contains(strings.ToLower(event.Message), "admission")) {

			// Extract details from the message
			denial := admissionDenial{
				ResourceKind: event.InvolvedObject.Kind,
				ResourceName: event.InvolvedObject.Name,
				Namespace:    event.InvolvedObject.Namespace,
				Message:      event.Message,
				Timestamp:    event.CreatedAt,
				Reason:       event.Reason,
				IsSystemNS:   r.isSystemNamespace(event.InvolvedObject.Namespace),
			}

			// Try to extract policy/webhook name
			denial.PolicyName = extractPolicyName(event.Message)
			denial.ServiceAccount = extractServiceAccount(event.Message)

			denials = append(denials, denial)
		}
	}

	// Check pod logs for admission denials
	for _, pod := range data.KubernetesData.Pods {
		// Check controller pods
		if isControllerPod(pod) {
			if logs, ok := data.KubernetesData.Logs[pod.Name]; ok {
				for _, line := range logs {
					if strings.Contains(strings.ToLower(line), "admission") &&
						(strings.Contains(strings.ToLower(line), "denied") ||
							strings.Contains(strings.ToLower(line), "forbidden")) {

						denials = append(denials, admissionDenial{
							ResourceKind: "Pod",
							ResourceName: pod.Name,
							Namespace:    pod.Namespace,
							Message:      line,
							Timestamp:    time.Now(),
							IsSystemNS:   r.isSystemNamespace(pod.Namespace),
							IsController: true,
						})
					}
				}
			}
		}
	}

	return denials
}

// checkServiceAccountDenials identifies denials affecting service accounts
func (r *AdmissionLockdownRule) checkServiceAccountDenials(denials []admissionDenial) []serviceAccountDenial {
	saMap := make(map[string]*serviceAccountDenial)

	for _, denial := range denials {
		if denial.ServiceAccount != "" {
			key := denial.Namespace + "/" + denial.ServiceAccount

			if existing, ok := saMap[key]; ok {
				existing.DenialCount++
				existing.LastDenial = denial.Timestamp
				existing.Resources = append(existing.Resources, denial.ResourceKind+"/"+denial.ResourceName)
			} else {
				saMap[key] = &serviceAccountDenial{
					ServiceAccount: denial.ServiceAccount,
					Namespace:      denial.Namespace,
					DenialCount:    1,
					FirstDenial:    denial.Timestamp,
					LastDenial:     denial.Timestamp,
					IsSystemSA:     strings.HasPrefix(denial.ServiceAccount, "system:") || denial.IsSystemNS,
					Resources:      []string{denial.ResourceKind + "/" + denial.ResourceName},
				}
			}
		}
	}

	// Convert map to slice
	var sadenials []serviceAccountDenial
	for _, sad := range saMap {
		sadenials = append(sadenials, *sad)
	}

	return sadenials
}

// checkControllerFailures identifies controller operation failures
func (r *AdmissionLockdownRule) checkControllerFailures(data *correlation.AnalysisData, windowStart time.Time, denials []admissionDenial) []controllerFailure {
	var failures []controllerFailure

	// Map of controller types to check
	controllers := map[string]string{
		"deployment":            "deployment-controller",
		"replicaset":            "replicaset-controller",
		"statefulset":           "statefulset-controller",
		"daemonset":             "daemonset-controller",
		"job":                   "job-controller",
		"cronjob":               "cronjob-controller",
		"service":               "service-controller",
		"endpoint":              "endpoint-controller",
		"persistentvolumeclaim": "pvc-controller",
	}

	// Check for controller-related denials
	controllerDenials := make(map[string]int)
	for _, denial := range denials {
		if denial.IsController {
			if controller, ok := controllers[strings.ToLower(denial.ResourceKind)]; ok {
				controllerDenials[controller]++
			}
		}
	}

	// Check controller pods
	for _, pod := range data.KubernetesData.Pods {
		if isControllerManagerPod(pod) || isSchedulerPod(pod) {
			// Check if controller is having issues
			for _, status := range pod.Status.ContainerStatuses {
				if status.RestartCount > 0 {
					// Check if restarts correlate with denials
					failures = append(failures, controllerFailure{
						ControllerName: pod.Name,
						Namespace:      pod.Namespace,
						FailureType:    "restart",
						RestartCount:   int(status.RestartCount),
						Message:        fmt.Sprintf("Controller restarted %d times", status.RestartCount),
						Timestamp:      time.Now(),
					})
				}
			}

			// Check logs for permission errors
			if logs, ok := data.KubernetesData.Logs[pod.Name]; ok {
				for _, line := range logs {
					if strings.Contains(strings.ToLower(line), "forbidden") ||
						strings.Contains(strings.ToLower(line), "unauthorized") ||
						strings.Contains(strings.ToLower(line), "permission denied") {

						failures = append(failures, controllerFailure{
							ControllerName: pod.Name,
							Namespace:      pod.Namespace,
							FailureType:    "permission",
							Message:        line,
							Timestamp:      time.Now(),
						})
						break
					}
				}
			}
		}
	}

	// Add denial-based failures
	for controller, count := range controllerDenials {
		if count > 2 {
			failures = append(failures, controllerFailure{
				ControllerName: controller,
				FailureType:    "admission_denials",
				DenialCount:    count,
				Message:        fmt.Sprintf("%d operations denied by admission control", count),
				Timestamp:      time.Now(),
			})
		}
	}

	return failures
}

// checkOperationalImpact assesses the operational impact of denials
func (r *AdmissionLockdownRule) checkOperationalImpact(data *correlation.AnalysisData, windowStart time.Time, denials []admissionDenial) operationalImpact {
	impact := operationalImpact{
		AffectedNamespaces: make(map[string]int),
		AffectedResources:  make(map[string]int),
		BlockedOperations:  make(map[string]int),
	}

	// Count affected namespaces and resources
	for _, denial := range denials {
		impact.AffectedNamespaces[denial.Namespace]++
		impact.AffectedResources[denial.ResourceKind]++

		// Try to categorize operation
		operation := categorizeOperation(denial.Message)
		impact.BlockedOperations[operation]++
	}

	// Check for stuck deployments
	for _, deployment := range data.KubernetesData.Deployments {
		if deployment.Status.Replicas < deployment.Status.UpdatedReplicas {
			impact.StuckDeployments++
		}
	}

	// Check for failed jobs
	for _, job := range data.KubernetesData.Jobs {
		if job.Status.Failed > 0 {
			// Check if failure is related to admission
			for _, event := range data.KubernetesData.Events {
				if event.InvolvedObject.Kind == "Job" &&
					event.InvolvedObject.Name == job.Name &&
					strings.Contains(strings.ToLower(event.Message), "admission") {
					impact.FailedJobs++
					break
				}
			}
		}
	}

	// Check for pending resources
	for _, pod := range data.KubernetesData.Pods {
		if pod.Status.Phase == "Pending" {
			// Check if pending due to admission
			for _, event := range data.KubernetesData.Events {
				if event.InvolvedObject.Kind == "Pod" &&
					event.InvolvedObject.Name == pod.Name &&
					event.CreatedAt.After(windowStart) &&
					strings.Contains(strings.ToLower(event.Message), "admission") {
					impact.PendingPods++
					break
				}
			}
		}
	}

	return impact
}

// calculateDenialStatistics calculates statistics about denials
func (r *AdmissionLockdownRule) calculateDenialStatistics(denials []admissionDenial) denialStatistics {
	stats := denialStatistics{
		TotalDenials:      len(denials),
		PolicyBreakdown:   make(map[string]int),
		ResourceBreakdown: make(map[string]int),
		TimeDistribution:  make(map[time.Duration]int),
	}

	if len(denials) == 0 {
		return stats
	}

	// Find time range
	earliest := denials[0].Timestamp
	latest := denials[0].Timestamp

	for _, denial := range denials {
		// Policy breakdown
		if denial.PolicyName != "" {
			stats.PolicyBreakdown[denial.PolicyName]++
		}

		// Resource breakdown
		stats.ResourceBreakdown[denial.ResourceKind]++

		// Track time range
		if denial.Timestamp.Before(earliest) {
			earliest = denial.Timestamp
		}
		if denial.Timestamp.After(latest) {
			latest = denial.Timestamp
		}

		// Count system namespace denials
		if denial.IsSystemNS {
			stats.SystemDenials++
		}
	}

	// Calculate rate
	duration := latest.Sub(earliest)
	if duration > 0 {
		stats.denialRate = float64(len(denials)) / duration.Minutes() * 100
	}

	// Time distribution (bucketed by 5-minute intervals)
	for _, denial := range denials {
		bucket := denial.Timestamp.Sub(earliest).Truncate(5 * time.Minute)
		stats.TimeDistribution[bucket]++
	}

	return stats
}

// Helper methods

func (r *AdmissionLockdownRule) calculateConfidence(stats denialStatistics, sadenials []serviceAccountDenial, controllers []controllerFailure, impact operationalImpact) float64 {
	confidence := 0.0

	// High denial rate indicates lockdown
	if stats.denialRate > r.config.CriticalDenialPercent {
		confidence = 0.4
	} else if stats.denialRate > r.config.CriticalDenialPercent/2 {
		confidence = 0.2
	}

	// Service account denials are strong indicator
	if len(sadenials) > 0 {
		confidence += 0.2
		// System service accounts are critical
		for _, sad := range sadenials {
			if sad.IsSystemSA {
				confidence += 0.1
				break
			}
		}
	}

	// Controller failures confirm impact
	if len(controllers) > 0 {
		confidence += 0.2
		if len(controllers) > 3 {
			confidence += 0.1
		}
	}

	// Operational impact shows severity
	if impact.StuckDeployments > 0 || impact.FailedJobs > 0 {
		confidence += 0.1
	}

	// System namespace denials are critical
	if stats.SystemDenials > 5 {
		confidence += 0.1
	}

	// Cap at 1.0
	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

func (r *AdmissionLockdownRule) buildDescription(stats denialStatistics, sadenials []serviceAccountDenial, controllers []controllerFailure, impact operationalImpact) string {
	parts := []string{
		fmt.Sprintf("Admission policies are blocking cluster operations with %.1f denials/minute.", stats.denialRate/100),
	}

	// Denial summary
	parts = append(parts, fmt.Sprintf("\n\n🚫 Denial Statistics:"))
	parts = append(parts, fmt.Sprintf("  - Total denials: %d", stats.TotalDenials))
	parts = append(parts, fmt.Sprintf("  - System namespace denials: %d", stats.SystemDenials))

	// Top policies causing denials
	if len(stats.PolicyBreakdown) > 0 {
		parts = append(parts, "\n📋 Top Blocking Policies:")
		count := 0
		for policy, denials := range stats.PolicyBreakdown {
			if count >= 3 {
				break
			}
			parts = append(parts, fmt.Sprintf("  - %s: %d denials", policy, denials))
			count++
		}
	}

	// Service account impact
	if len(sadenials) > 0 {
		parts = append(parts, fmt.Sprintf("\n\n🔐 Service Account Denials (%d affected):", len(sadenials)))
		systemSAs := 0
		for _, sad := range sadenials {
			if sad.IsSystemSA {
				systemSAs++
			}
		}
		if systemSAs > 0 {
			parts = append(parts, fmt.Sprintf("  - %d system service accounts blocked", systemSAs))
		}
		parts = append(parts, fmt.Sprintf("  - Controllers unable to manage resources"))
	}

	// Controller failures
	if len(controllers) > 0 {
		parts = append(parts, fmt.Sprintf("\n\n⚠️  Controller Impact (%d affected):", len(controllers)))
		for i, failure := range controllers {
			if i >= 3 {
				break
			}
			parts = append(parts, fmt.Sprintf("  - %s: %s", failure.ControllerName, failure.Message))
		}
	}

	// Operational impact
	if impact.StuckDeployments > 0 || impact.FailedJobs > 0 || impact.PendingPods > 0 {
		parts = append(parts, "\n\n📊 Operational Impact:")
		if impact.StuckDeployments > 0 {
			parts = append(parts, fmt.Sprintf("  - %d deployments stuck", impact.StuckDeployments))
		}
		if impact.FailedJobs > 0 {
			parts = append(parts, fmt.Sprintf("  - %d jobs failed", impact.FailedJobs))
		}
		if impact.PendingPods > 0 {
			parts = append(parts, fmt.Sprintf("  - %d pods pending", impact.PendingPods))
		}
	}

	parts = append(parts, "\n\n⚡ Pattern: Policy enforcement → Service account denials → Controller failures → Operational paralysis")

	return strings.Join(parts, "\n")
}

func (r *AdmissionLockdownRule) determineSeverity(stats denialStatistics, sadenials []serviceAccountDenial, controllers []controllerFailure) correlation.Severity {
	// System service account denials are critical
	for _, sad := range sadenials {
		if sad.IsSystemSA && sad.DenialCount > 5 {
			return correlation.SeverityCritical
		}
	}

	// High denial rate with controller failures
	if stats.denialRate > r.config.CriticalDenialPercent && len(controllers) > 0 {
		return correlation.SeverityCritical
	}

	// Many system namespace denials
	if stats.SystemDenials > 10 {
		return correlation.SeverityCritical
	}

	return correlation.SeverityHigh
}

func (r *AdmissionLockdownRule) assessImpact(stats denialStatistics, controllers []controllerFailure, impact operationalImpact) string {
	if len(controllers) > 5 && impact.StuckDeployments > 5 {
		return "Critical: Cluster operations paralyzed, no changes can be made"
	} else if stats.SystemDenials > 10 {
		return "Severe: System components blocked, cluster stability at risk"
	} else if impact.StuckDeployments > 0 || impact.FailedJobs > 0 {
		return "High: Application deployments blocked, operations impaired"
	}
	return "Moderate: Some operations blocked by admission policies"
}

func (r *AdmissionLockdownRule) identifyRootCause(denials []admissionDenial, sadenials []serviceAccountDenial) string {
	// Check for policy changes
	policyTypes := make(map[string]bool)
	for _, denial := range denials {
		if denial.PolicyName != "" {
			if strings.Contains(denial.PolicyName, "psp") || strings.Contains(denial.PolicyName, "podsecurity") {
				policyTypes["pod-security"] = true
			} else if strings.Contains(denial.PolicyName, "networkpolicy") {
				policyTypes["network"] = true
			} else if strings.Contains(denial.PolicyName, "opa") || strings.Contains(denial.PolicyName, "gatekeeper") {
				policyTypes["opa"] = true
			}
		}
	}

	if len(policyTypes) > 1 {
		return "Multiple admission policies enforcing conflicting or overly restrictive rules"
	} else if policyTypes["pod-security"] {
		return "Pod Security Standards/Policies blocking workload creation"
	} else if policyTypes["opa"] {
		return "OPA/Gatekeeper policies too restrictive for normal operations"
	}

	// Check for RBAC issues
	systemSABlocked := false
	for _, sad := range sadenials {
		if sad.IsSystemSA {
			systemSABlocked = true
			break
		}
	}

	if systemSABlocked {
		return "System service accounts lacking required permissions after policy changes"
	}

	return "Admission control policies preventing legitimate cluster operations"
}

func (r *AdmissionLockdownRule) collectEvidence(stats denialStatistics, sadenials []serviceAccountDenial, controllers []controllerFailure, impact operationalImpact) []string {
	evidence := []string{
		fmt.Sprintf("%.0f denials per minute detected", stats.denialRate/100),
		fmt.Sprintf("%d total denials in analysis window", stats.TotalDenials),
	}

	if stats.SystemDenials > 0 {
		evidence = append(evidence, fmt.Sprintf("%d denials in system namespaces", stats.SystemDenials))
	}

	if len(sadenials) > 0 {
		evidence = append(evidence, fmt.Sprintf("%d service accounts experiencing denials", len(sadenials)))
	}

	if len(controllers) > 0 {
		evidence = append(evidence, fmt.Sprintf("%d controllers affected by admission denials", len(controllers)))
	}

	if impact.StuckDeployments > 0 {
		evidence = append(evidence, fmt.Sprintf("%d deployments unable to progress", impact.StuckDeployments))
	}

	// Top denied resources
	topResource := ""
	maxDenials := 0
	for resource, count := range stats.ResourceBreakdown {
		if count > maxDenials {
			topResource = resource
			maxDenials = count
		}
	}
	if topResource != "" {
		evidence = append(evidence, fmt.Sprintf("Most denied resource type: %s (%d denials)", topResource, maxDenials))
	}

	return evidence
}

func (r *AdmissionLockdownRule) predictTimeToParalysis(stats denialStatistics, controllers []controllerFailure) time.Duration {
	// Already paralyzed
	if len(controllers) > 5 {
		return 0
	}

	// Based on denial rate acceleration
	if stats.denialRate > r.config.CriticalDenialPercent*2 {
		return 5 * time.Minute
	} else if stats.denialRate > r.config.CriticalDenialPercent {
		return 15 * time.Minute
	}

	return 30 * time.Minute
}

func (r *AdmissionLockdownRule) identifyPolicyResources(denials []admissionDenial) []correlation.ResourceReference {
	policyMap := make(map[string]correlation.ResourceReference)

	for _, denial := range denials {
		if denial.PolicyName != "" {
			// Try to identify the policy resource
			if strings.Contains(denial.PolicyName, ".") {
				// Webhook name format: name.namespace.svc
				parts := strings.Split(denial.PolicyName, ".")
				if len(parts) >= 2 {
					key := parts[0]
					if _, ok := policyMap[key]; !ok {
						policyMap[key] = correlation.ResourceReference{
							Kind:      "ValidatingWebhookConfiguration",
							Name:      parts[0],
							Namespace: "", // Cluster-scoped
						}
					}
				}
			}
		}
	}

	var resources []correlation.ResourceReference
	for _, ref := range policyMap {
		resources = append(resources, ref)
	}

	return resources
}

// Helper functions

func (r *AdmissionLockdownRule) isSystemNamespace(namespace string) bool {
	for _, sysNs := range r.config.SystemNamespaces {
		if namespace == sysNs {
			return true
		}
	}
	return strings.HasPrefix(namespace, "kube-")
}

func isControllerPod(pod types.PodInfo) bool {
	// Check if it's a known controller
	controllerNames := []string{
		"controller-manager",
		"deployment-controller",
		"replicaset-controller",
		"job-controller",
		"cronjob-controller",
		"statefulset-controller",
		"daemonset-controller",
	}

	for _, name := range controllerNames {
		if strings.Contains(pod.Name, name) {
			return true
		}
	}

	// Check labels
	if component, ok := pod.Labels["component"]; ok {
		return strings.Contains(component, "controller")
	}

	return false
}

func extractPolicyName(message string) string {
	// Try to extract webhook/policy name from message
	patterns := []string{
		"admission webhook \"",
		"denied by ",
		"policy ",
		"webhook ",
	}

	msg := strings.ToLower(message)
	for _, pattern := range patterns {
		if idx := strings.Index(msg, pattern); idx >= 0 {
			start := idx + len(pattern)
			end := strings.IndexAny(message[start:], "\" ,;:")
			if end > 0 {
				return message[start : start+end]
			}
		}
	}

	return ""
}

func extractServiceAccount(message string) string {
	// Extract service account from denial message
	if idx := strings.Index(message, "serviceaccount:"); idx >= 0 {
		start := idx + len("serviceaccount:")
		parts := strings.Fields(message[start:])
		if len(parts) > 0 {
			// Format: namespace:name
			sa := strings.TrimPrefix(parts[0], "\"")
			sa = strings.TrimSuffix(sa, "\"")
			return sa
		}
	}

	// Alternative format
	if idx := strings.Index(message, "service account"); idx >= 0 {
		parts := strings.Fields(message[idx:])
		if len(parts) > 2 {
			return strings.Trim(parts[2], "\"'")
		}
	}

	return ""
}

func categorizeOperation(message string) string {
	msg := strings.ToLower(message)

	operations := map[string][]string{
		"create": {"create", "creating", "creation"},
		"update": {"update", "updating", "patch", "patching"},
		"delete": {"delete", "deleting", "deletion"},
		"scale":  {"scale", "scaling", "replica"},
		"exec":   {"exec", "attach", "portforward"},
	}

	for op, keywords := range operations {
		for _, keyword := range keywords {
			if strings.Contains(msg, keyword) {
				return op
			}
		}
	}

	return "unknown"
}

// Helper types
type admissionDenial struct {
	ResourceKind   string
	ResourceName   string
	Namespace      string
	PolicyName     string
	ServiceAccount string
	Message        string
	Reason         string
	Timestamp      time.Time
	IsSystemNS     bool
	IsController   bool
}

type serviceAccountDenial struct {
	ServiceAccount string
	Namespace      string
	DenialCount    int
	FirstDenial    time.Time
	LastDenial     time.Time
	IsSystemSA     bool
	Resources      []string
}

type controllerFailure struct {
	ControllerName string
	Namespace      string
	FailureType    string // "restart", "permission", "admission_denials"
	RestartCount   int
	DenialCount    int
	Message        string
	Timestamp      time.Time
}

type operationalImpact struct {
	AffectedNamespaces map[string]int
	AffectedResources  map[string]int
	BlockedOperations  map[string]int
	StuckDeployments   int
	FailedJobs         int
	PendingPods        int
}

type denialStatistics struct {
	TotalDenials      int
	SystemDenials     int
	PolicyBreakdown   map[string]int
	ResourceBreakdown map[string]int
	TimeDistribution  map[time.Duration]int
	denialRate        float64 // denials per minute * 100
}
