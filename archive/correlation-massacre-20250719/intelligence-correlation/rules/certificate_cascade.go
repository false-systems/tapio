package rules
import (
	"context"
	"fmt"
	"strings"
	"time"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	"github.com/yairfalse/tapio/pkg/domain"
)
// CertificateCascadeRule detects certificate chain failures that cascade through the system
// Pattern: Root cert renewal → webhook cert mismatch → admission failures → deployment blocks
type CertificateCascadeRule struct {
	*correlation.BaseRule
	config CertificateCascadeConfig
}
// CertificateCascadeConfig configures the certificate cascade detection rule
type CertificateCascadeConfig struct {
	// Time before expiry to trigger warnings (hours)
	ExpiryWarningHours int
	// Minimum confidence required
	MinConfidence float64
	// Time window for correlation
	TimeWindow time.Duration
	// Minimum webhook failures to trigger
	MinWebhookFailures int
}
// DefaultCertificateCascadeConfig returns the default configuration
func DefaultCertificateCascadeConfig() CertificateCascadeConfig {
	return CertificateCascadeConfig{
		ExpiryWarningHours: 72, // 3 days
		MinConfidence:      0.70,
		TimeWindow:         30 * time.Minute,
		MinWebhookFailures: 3,
	}
}
// NewCertificateCascadeRule creates a new certificate cascade detection rule
func NewCertificateCascadeRule(config CertificateCascadeConfig) *CertificateCascadeRule {
	metadata := correlation.RuleMetadata{
		ID:          "certificate_chain_failure",
		Name:        "Certificate Chain Failure Detection",
		Description: "Detects when certificate expiry or renewal issues cascade through webhooks causing admission controller failures and deployment blockages",
		Version:     "1.0.0",
		Author:      "Tapio Correlation Engine",
		Tags:        []string{"certificates", "webhooks", "cascade", "security"},
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
	return &CertificateCascadeRule{
		BaseRule: correlation.NewBaseRule(metadata),
		config:   config,
	}
}
// CheckRequirements verifies that required data sources are available
func (r *CertificateCascadeRule) CheckRequirements(ctx context.Context, data *correlation.DataCollection) error {
	if !data.IsSourceAvailable(correlation.SourceKubernetes) {
		return correlation.NewRequirementNotMetError(r.GetID(), r.GetMetadata().Requirements[0])
	}
	return nil
}
// GetConfidenceFactors returns factors that affect confidence scoring
func (r *CertificateCascadeRule) GetConfidenceFactors() []string {
	return []string{
		"certificate_expiry_proximity",
		"webhook_failure_correlation",
		"admission_controller_patterns",
		"deployment_blockage_severity",
		"certificate_chain_integrity",
	}
}
// Validate validates the rule configuration
func (r *CertificateCascadeRule) Validate() error {
	if r.config.ExpiryWarningHours <= 0 {
		return correlation.NewRuleValidationError("expiry_warning_hours must be positive")
	}
	if r.config.MinWebhookFailures <= 0 {
		return correlation.NewRuleValidationError("min_webhook_failures must be positive")
	}
	if r.config.TimeWindow <= 0 {
		return correlation.NewRuleValidationError("time_window must be positive")
	}
	if r.config.MinConfidence <= 0 || r.config.MinConfidence > 1 {
		return correlation.NewRuleValidationError("min_confidence must be between 0 and 1")
	}
	return nil
}
// Execute runs the certificate cascade detection logic
func (r *CertificateCascadeRule) Execute(ctx context.Context, ruleCtx *correlation.RuleContext) ([]correlation.Finding, error) {
	var findings []correlation.Finding
	// Get Kubernetes data from rule context
	k8sData, err := ruleCtx.DataCollection.GetKubernetesData(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Kubernetes data: %w", err)
	}
	// Get current time for analysis
	now := time.Now()
	windowStart := now.Add(-r.config.TimeWindow)
	// Check for certificate issues
	certIssues := r.checkCertificateIssues(k8sData, windowStart)
	if len(certIssues) == 0 {
		return findings, nil // No certificate issues
	}
	// Check for webhook failures
	webhookFailures := r.checkWebhookFailures(k8sData, windowStart)
	// Check for admission controller errors
	admissionErrors := r.checkAdmissionErrors(k8sData, windowStart)
	// Check for deployment failures
	deploymentFailures := r.checkDeploymentFailures(k8sData, windowStart)
	// Correlate findings
	if len(certIssues) > 0 && len(webhookFailures) >= r.config.MinWebhookFailures {
		confidence := r.calculateConfidence(certIssues, webhookFailures, admissionErrors, deploymentFailures)
		if confidence >= r.config.MinConfidence {
			finding := correlation.Finding{
				RuleID:      r.ID(),
				Title:       "Certificate Chain Failure Cascade",
				Description: r.buildDescription(certIssues, webhookFailures, admissionErrors, deploymentFailures),
				Severity:    r.determineSeverity(certIssues, webhookFailures),
				Confidence:  confidence,
				Impact:      r.assessImpact(webhookFailures, admissionErrors, deploymentFailures),
				RootCause:   r.identifyRootCause(certIssues),
				Evidence:    r.collectEvidence(certIssues, webhookFailures, admissionErrors, deploymentFailures),
				Recommendations: []string{
					"Check and renew expiring certificates immediately",
					"Verify certificate chain validity across all components",
					"Update webhook configurations with correct CA bundles",
					"Consider implementing cert-manager for automatic renewal",
					"Review and fix any manual certificate deployments",
					"Restart affected webhook pods after certificate updates",
				},
				Prediction: &correlation.RulePrediction{
					Event:       "Complete admission control failure",
					TimeToEvent: r.predictTimeToFailure(certIssues),
					Confidence:  confidence,
				},
			}
			// Add affected resources
			for _, issue := range certIssues {
				finding.AffectedResources = append(finding.AffectedResources, correlation.ResourceReference{
					Kind:      issue.ResourceKind,
					Name:      issue.ResourceName,
					Namespace: issue.Namespace,
				})
			}
			for _, failure := range webhookFailures {
				finding.AffectedResources = append(finding.AffectedResources, correlation.ResourceReference{
					Kind:      "Pod",
					Name:      failure.PodName,
					Namespace: failure.Namespace,
				})
			}
			findings = append(findings, finding)
		}
	}
	return findings, nil
}
// checkCertificateIssues identifies certificate problems
func (r *CertificateCascadeRule) checkCertificateIssues(k8sData *correlation.KubernetesData, windowStart time.Time) []certIssue {
	var issues []certIssue
	// Check control plane components
	controlPlaneComponents := []string{"kube-apiserver", "kube-controller-manager", "kube-scheduler"}
	for _, pod := range k8sData.Pods {
		// Convert Pod to PodInfo
		podInfo := convertPodToPodInfo(pod)
		// Check control plane pods
		isControlPlane := false
		componentName := ""
		for _, comp := range controlPlaneComponents {
			if strings.Contains(pod.Name, comp) {
				isControlPlane = true
				componentName = comp
				break
			}
		}
		// Check for certificate-related events or logs
		if certInfo, found := getPodCertificateExpiry(podInfo, k8sData); found {
			issues = append(issues, certIssue{
				ResourceKind:   "Pod",
				ResourceName:   pod.Name,
				Namespace:      pod.Namespace,
				Component:      componentName,
				IsControlPlane: isControlPlane,
				Message:        certInfo,
				Timestamp:      time.Now(),
			})
		}
		// Check events for certificate issues
		for _, event := range k8sData.Events {
			if event.InvolvedObject.Name == pod.Name &&
				event.CreationTimestamp.Time.After(windowStart) &&
				(strings.Contains(strings.ToLower(event.Message), "certificate") ||
					strings.Contains(strings.ToLower(event.Message), "x509") ||
					strings.Contains(strings.ToLower(event.Message), "tls")) {
				issues = append(issues, certIssue{
					ResourceKind:   "Pod",
					ResourceName:   pod.Name,
					Namespace:      pod.Namespace,
					Component:      componentName,
					IsControlPlane: isControlPlane,
					Message:        event.Message,
					Timestamp:      event.CreationTimestamp.Time,
				})
			}
		}
	}
	// Check secrets for certificate expiry
	for _, secret := range k8sData.Secrets {
		if secret.Type == "kubernetes.io/tls" {
			// Check for recent update events that might indicate renewal issues
			for _, event := range k8sData.Events {
				if event.InvolvedObject.Kind == "Secret" &&
					event.InvolvedObject.Name == secret.Name &&
					event.CreationTimestamp.Time.After(windowStart) &&
					strings.Contains(strings.ToLower(event.Message), "invalid") {
					issues = append(issues, certIssue{
						ResourceKind: "Secret",
						ResourceName: secret.Name,
						Namespace:    secret.Namespace,
						Message:      event.Message,
						Timestamp:    event.CreationTimestamp.Time,
					})
				}
			}
		}
	}
	return issues
}
// checkWebhookFailures identifies webhook-related failures
func (r *CertificateCascadeRule) checkWebhookFailures(k8sData *correlation.KubernetesData, windowStart time.Time) []webhookFailure {
	var failures []webhookFailure
	// Check for webhook pods
	for _, pod := range k8sData.Pods {
		podInfo := convertPodToPodInfo(pod)
		if !isAdmissionWebhook(podInfo) {
			continue
		}
		// Check pod status
		for _, status := range pod.Status.ContainerStatuses {
			if !status.Ready || status.RestartCount > 0 {
				// Check logs for certificate errors
				if logs, ok := k8sData.Logs[pod.Name]; ok {
					for _, logEntry := range logs {
						if strings.Contains(strings.ToLower(logEntry.Message), "certificate") ||
							strings.Contains(strings.ToLower(logEntry.Message), "x509") ||
							strings.Contains(strings.ToLower(logEntry.Message), "tls handshake") {
							failures = append(failures, webhookFailure{
								PodName:      pod.Name,
								Namespace:    pod.Namespace,
								WebhookName:  extractWebhookName(podInfo),
								RestartCount: int(status.RestartCount),
								Ready:        status.Ready,
								Error:        logEntry.Message,
								Timestamp:    logEntry.Timestamp,
							})
							break
						}
					}
				}
			}
		}
		// Check events
		for _, event := range k8sData.Events {
			if event.InvolvedObject.Name == pod.Name &&
				event.CreationTimestamp.Time.After(windowStart) &&
				(strings.Contains(strings.ToLower(event.Message), "failed") ||
					strings.Contains(strings.ToLower(event.Message), "error")) {
				failures = append(failures, webhookFailure{
					PodName:     pod.Name,
					Namespace:   pod.Namespace,
					WebhookName: extractWebhookName(podInfo),
					Error:       event.Message,
					Timestamp:   event.CreationTimestamp.Time,
				})
			}
		}
	}
	return failures
}
// checkAdmissionErrors looks for admission controller errors
func (r *CertificateCascadeRule) checkAdmissionErrors(k8sData *correlation.KubernetesData, windowStart time.Time) []admissionError {
	var errors []admissionError
	// Look for admission webhook errors in events
	for _, event := range k8sData.Events {
		if event.CreationTimestamp.Time.After(windowStart) &&
			(strings.Contains(strings.ToLower(event.Message), "admission webhook") ||
				strings.Contains(strings.ToLower(event.Message), "admission controller") ||
				strings.Contains(strings.ToLower(event.Message), "validating webhook") ||
				strings.Contains(strings.ToLower(event.Message), "mutating webhook")) &&
			(strings.Contains(strings.ToLower(event.Message), "failed") ||
				strings.Contains(strings.ToLower(event.Message), "error") ||
				strings.Contains(strings.ToLower(event.Message), "denied")) {
			errors = append(errors, admissionError{
				ResourceKind: event.InvolvedObject.Kind,
				ResourceName: event.InvolvedObject.Name,
				Namespace:    event.InvolvedObject.Namespace,
				WebhookName:  extractWebhookFromMessage(event.Message),
				Error:        event.Message,
				Timestamp:    event.CreationTimestamp.Time,
			})
		}
	}
	return errors
}
// checkDeploymentFailures identifies deployment failures due to admission issues
func (r *CertificateCascadeRule) checkDeploymentFailures(k8sData *correlation.KubernetesData, windowStart time.Time) []deploymentFailure {
	var failures []deploymentFailure
	// Check deployments
	for _, deployment := range k8sData.Deployments {
		// Check if deployment is progressing
		failed := false
		for _, condition := range deployment.Status.Conditions {
			if condition.Type == "Progressing" && condition.Status == "False" {
				failed = true
			}
		}
		if failed || (deployment.Status.Replicas == 0 && deployment.Spec.Replicas != nil && *deployment.Spec.Replicas > 0) {
			// Check events for admission-related failures
			for _, event := range k8sData.Events {
				if event.InvolvedObject.Kind == "Deployment" &&
					event.InvolvedObject.Name == deployment.Name &&
					event.CreationTimestamp.Time.After(windowStart) &&
					(strings.Contains(strings.ToLower(event.Message), "admission") ||
						strings.Contains(strings.ToLower(event.Message), "webhook")) {
					failures = append(failures, deploymentFailure{
						DeploymentName:  deployment.Name,
						Namespace:       deployment.Namespace,
						DesiredReplicas: deployment.Spec.Replicas,
						ActualReplicas:  deployment.Status.Replicas,
						Reason:          event.Message,
						Timestamp:       event.CreationTimestamp.Time,
					})
				}
			}
		}
	}
	// Check ReplicaSets for more detailed errors
	for _, rs := range k8sData.ReplicaSets {
		if rs.Status.Replicas == 0 && rs.Spec.Replicas != nil && *rs.Spec.Replicas > 0 {
			for _, event := range k8sData.Events {
				if event.InvolvedObject.Kind == "ReplicaSet" &&
					event.InvolvedObject.Name == rs.Name &&
					event.CreationTimestamp.Time.After(windowStart) &&
					strings.Contains(strings.ToLower(event.Message), "admission") {
					// Find parent deployment
					for _, owner := range rs.OwnerReferences {
						if owner.Kind == "Deployment" {
							failures = append(failures, deploymentFailure{
								DeploymentName: owner.Name,
								Namespace:      rs.Namespace,
								Reason:         event.Message,
								Timestamp:      event.CreationTimestamp.Time,
							})
						}
					}
				}
			}
		}
	}
	return failures
}
// Helper methods
func (r *CertificateCascadeRule) calculateConfidence(certs []certIssue, webhooks []webhookFailure, admission []admissionError, deployments []deploymentFailure) float64 {
	confidence := 0.0
	// Certificate issues are the root cause
	if len(certs) > 0 {
		confidence = 0.3
		// Higher confidence for control plane certificates
		for _, cert := range certs {
			if cert.IsControlPlane {
				confidence += 0.1
				break
			}
		}
	}
	// Webhook failures strongly indicate certificate cascade
	if len(webhooks) > 0 {
		confidence += 0.3
		if len(webhooks) > 5 {
			confidence += 0.1
		}
	}
	// Admission errors confirm the cascade
	if len(admission) > 0 {
		confidence += 0.2
	}
	// Deployment failures show full impact
	if len(deployments) > 0 {
		confidence += 0.2
	}
	// Cap at 1.0
	if confidence > 1.0 {
		confidence = 1.0
	}
	return confidence
}
func (r *CertificateCascadeRule) buildDescription(certs []certIssue, webhooks []webhookFailure, admission []admissionError, deployments []deploymentFailure) string {
	parts := []string{
		"Certificate chain failure detected causing cascading webhook and admission control failures.",
	}
	if len(certs) > 0 {
		parts = append(parts, fmt.Sprintf("\n\n🔐 Certificate Issues (%d found):", len(certs)))
		for i, cert := range certs {
			if i >= 3 {
				parts = append(parts, fmt.Sprintf("  ... and %d more", len(certs)-3))
				break
			}
			parts = append(parts, fmt.Sprintf("  - %s/%s: %s", cert.ResourceKind, cert.ResourceName, truncateMessage(cert.Message, 60)))
		}
	}
	if len(webhooks) > 0 {
		parts = append(parts, fmt.Sprintf("\n\n🔌 Webhook Failures (%d webhooks):", len(webhooks)))
		uniqueWebhooks := make(map[string]bool)
		for _, wh := range webhooks {
			uniqueWebhooks[wh.WebhookName] = true
		}
		count := 0
		for webhook := range uniqueWebhooks {
			if count >= 3 {
				parts = append(parts, fmt.Sprintf("  ... and %d more", len(uniqueWebhooks)-3))
				break
			}
			parts = append(parts, fmt.Sprintf("  - %s: certificate validation failures", webhook))
			count++
		}
	}
	if len(admission) > 0 {
		parts = append(parts, fmt.Sprintf("\n\n⚠️  Admission Controller Impact (%d errors):", len(admission)))
		parts = append(parts, "  - Resources being rejected by admission webhooks")
		parts = append(parts, "  - New deployments and updates blocked")
	}
	if len(deployments) > 0 {
		parts = append(parts, fmt.Sprintf("\n\n📦 Deployment Failures (%d affected):", len(deployments)))
		for i, dep := range deployments {
			if i >= 3 {
				parts = append(parts, fmt.Sprintf("  ... and %d more", len(deployments)-3))
				break
			}
			parts = append(parts, fmt.Sprintf("  - %s: unable to create pods", dep.DeploymentName))
		}
	}
	parts = append(parts, "\n\n⚡ Cascade: Certificate expiry → Webhook TLS failures → Admission denials → Deployment blocks")
	return strings.Join(parts, "\n")
}
func (r *CertificateCascadeRule) determineSeverity(certs []certIssue, webhooks []webhookFailure) correlation.Severity {
	// Control plane certificates are always critical
	for _, cert := range certs {
		if cert.IsControlPlane {
			return correlation.SeverityLevelCritical
		}
	}
	// Many webhook failures indicate critical issue
	if len(webhooks) > 5 {
		return correlation.SeverityLevelCritical
	}
	// Otherwise high severity
	return correlation.SeverityLevelError
}
func (r *CertificateCascadeRule) assessImpact(webhooks []webhookFailure, admission []admissionError, deployments []deploymentFailure) string {
	if len(deployments) > 10 {
		return "Severe: Multiple deployments blocked, no new workloads can be created"
	} else if len(admission) > 20 {
		return "Critical: Admission control failing, cluster operations severely impaired"
	} else if len(webhooks) > 5 {
		return "High: Multiple webhooks offline, some operations may fail"
	}
	return "Moderate: Certificate issues detected, intervention required to prevent escalation"
}
func (r *CertificateCascadeRule) identifyRootCause(certs []certIssue) string {
	controlPlaneCerts := 0
	webhookCerts := 0
	for _, cert := range certs {
		if cert.IsControlPlane {
			controlPlaneCerts++
		} else if strings.Contains(cert.Message, "webhook") {
			webhookCerts++
		}
	}
	if controlPlaneCerts > 0 {
		return "Control plane certificate expiry or misconfiguration"
	} else if webhookCerts > 0 {
		return "Webhook certificate mismatch or expiry"
	}
	return "Certificate chain validation failure"
}
func (r *CertificateCascadeRule) collectEvidence(certs []certIssue, webhooks []webhookFailure, admission []admissionError, deployments []deploymentFailure) []correlation.RuleEvidence {
	evidence := []correlation.RuleEvidence{}
	// Certificate evidence
	for i, cert := range certs {
		if i >= 3 {
			evidence = append(evidence, correlation.RuleEvidence{
				Type:        "certificate_issue_summary",
				Source:      correlation.SourceKubernetes,
				Description: fmt.Sprintf("... and %d more certificate issues", len(certs)-3),
				Data: map[string]interface{}{
					"remaining_count": len(certs) - 3,
				},
				Timestamp:  time.Now(),
				Confidence: 0.9,
			})
			break
		}
		evidence = append(evidence, correlation.RuleEvidence{
			Type:        "certificate_issue",
			Source:      correlation.SourceKubernetes,
			Description: fmt.Sprintf("Certificate issue in %s/%s", cert.ResourceKind, cert.ResourceName),
			Data: map[string]interface{}{
				"resource_kind": cert.ResourceKind,
				"resource_name": cert.ResourceName,
				"namespace":     cert.Namespace,
				"message":       cert.Message,
			},
			Timestamp:  cert.Timestamp,
			Confidence: 0.95,
		})
	}
	// Webhook evidence
	if len(webhooks) > 0 {
		evidence = append(evidence, correlation.RuleEvidence{
			Type:        "webhook_failures",
			Source:      correlation.SourceKubernetes,
			Description: fmt.Sprintf("%d webhooks experiencing TLS/certificate failures", len(webhooks)),
			Data: map[string]interface{}{
				"webhook_count": len(webhooks),
			},
			Timestamp:  time.Now(),
			Confidence: 0.9,
		})
	}
	// Admission evidence
	if len(admission) > 0 {
		evidence = append(evidence, correlation.RuleEvidence{
			Type:        "admission_errors",
			Source:      correlation.SourceKubernetes,
			Description: fmt.Sprintf("%d admission control errors recorded", len(admission)),
			Data: map[string]interface{}{
				"error_count": len(admission),
			},
			Timestamp:  time.Now(),
			Confidence: 0.85,
		})
	}
	// Deployment evidence
	if len(deployments) > 0 {
		evidence = append(evidence, correlation.RuleEvidence{
			Type:        "deployment_failures",
			Source:      correlation.SourceKubernetes,
			Description: fmt.Sprintf("%d deployments blocked by admission failures", len(deployments)),
			Data: map[string]interface{}{
				"deployment_count": len(deployments),
			},
			Timestamp:  time.Now(),
			Confidence: 0.8,
		})
	}
	return evidence
}
func (r *CertificateCascadeRule) predictTimeToFailure(certs []certIssue) time.Duration {
	// Look for expiry messages
	for _, cert := range certs {
		if strings.Contains(strings.ToLower(cert.Message), "expired") {
			return 0 // Already expired
		}
		if strings.Contains(strings.ToLower(cert.Message), "expir") {
			// Try to extract time
			if strings.Contains(cert.Message, "hour") {
				return 1 * time.Hour
			} else if strings.Contains(cert.Message, "day") {
				return 24 * time.Hour
			}
		}
	}
	// Default prediction based on typical cert renewal patterns
	return time.Duration(r.config.ExpiryWarningHours) * time.Hour
}
// Helper types
type certIssue struct {
	ResourceKind   string
	ResourceName   string
	Namespace      string
	Component      string
	IsControlPlane bool
	Message        string
	Timestamp      time.Time
}
type webhookFailure struct {
	PodName      string
	Namespace    string
	WebhookName  string
	RestartCount int
	Ready        bool
	Error        string
	Timestamp    time.Time
}
type admissionError struct {
	ResourceKind string
	ResourceName string
	Namespace    string
	WebhookName  string
	Error        string
	Timestamp    time.Time
}
type deploymentFailure struct {
	DeploymentName  string
	Namespace       string
	DesiredReplicas *int32
	ActualReplicas  int32
	Reason          string
	Timestamp       time.Time
}
// Utility functions
func extractWebhookName(pod PodInfo) string {
	// Try to extract from labels
	if name, ok := pod.Labels["webhook"]; ok {
		return name
	}
	if name, ok := pod.Labels["app"]; ok {
		return name
	}
	// Fallback to pod name
	parts := strings.Split(pod.Name, "-")
	if len(parts) > 2 {
		return strings.Join(parts[:len(parts)-2], "-")
	}
	return pod.Name
}
func extractWebhookFromMessage(message string) string {
	// Try to extract webhook name from error message
	if idx := strings.Index(message, "admission webhook"); idx >= 0 {
		parts := strings.Split(message[idx:], " ")
		if len(parts) > 2 {
			webhook := strings.Trim(parts[2], "\"'")
			return webhook
		}
	}
	return "unknown"
}
func truncateMessage(msg string, maxLen int) string {
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen-3] + "..."
}
