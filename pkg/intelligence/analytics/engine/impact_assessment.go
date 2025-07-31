package engine

import (
	"context"
	"strings"

	"github.com/yairfalse/tapio/pkg/domain"
)

// ImpactAssessment evaluates the technical and infrastructure impact of events
type ImpactAssessment struct {
	serviceGraph map[string][]string // service dependencies
}

// NewImpactAssessment creates a new impact assessment component
func NewImpactAssessment() *ImpactAssessment {
	return &ImpactAssessment{
		serviceGraph: make(map[string][]string),
	}
}

// Assess evaluates the impact of an event
func (ia *ImpactAssessment) Assess(ctx context.Context, event *domain.UnifiedEvent) (*ImpactResult, error) {
	result := &ImpactResult{
		TechnicalSeverity:    "medium", // default
		InfrastructureImpact: 0.5,      // default
		CascadeRisk:          0.0,
		AffectedServices:     []string{},
		RecommendedActions:   []string{},
	}

	// Use event's impact context if available
	if event.Impact != nil {
		result.TechnicalSeverity = event.Impact.Severity
		result.InfrastructureImpact = event.Impact.InfrastructureImpact
		result.AffectedServices = event.Impact.AffectedServices

		// Calculate cascade risk based on technical indicators
		if event.Impact.SLOImpact {
			result.CascadeRisk += 0.3
		}
		if event.Impact.CascadeRisk {
			result.CascadeRisk += 0.2
		}
	}

	// Layer-specific impact assessment
	if event.IsKernelEvent() && event.Kernel != nil {
		ia.assessKernelImpact(event.Kernel, result)
	}

	if event.IsNetworkEvent() && event.Network != nil {
		ia.assessNetworkImpact(event.Network, result)
	}

	if event.IsApplicationEvent() && event.Application != nil {
		ia.assessApplicationImpact(event.Application, result)
	}

	if event.IsKubernetesEvent() && event.Kubernetes != nil {
		ia.assessKubernetesImpact(event.Kubernetes, result)
	}

	// Adjust severity based on entity type and infrastructure role
	if event.Entity != nil {
		switch event.Entity.Type {
		case "node":
			result.InfrastructureImpact *= 2.0 // Nodes affect all pods
			result.CascadeRisk += 0.5
		case "service", "ingress", "networkpolicy":
			result.InfrastructureImpact *= 1.5 // Network resources affect connectivity
			result.CascadeRisk += 0.3
		case "deployment", "statefulset", "daemonset":
			result.InfrastructureImpact *= 1.3 // Workload controllers
			result.CascadeRisk += 0.2
		case "configmap", "secret":
			result.InfrastructureImpact *= 1.1 // Configuration changes
		}

		// System namespace detection
		if event.Entity.Namespace == "kube-system" || event.Entity.Namespace == "kube-public" {
			result.InfrastructureImpact *= 1.5
			result.TechnicalSeverity = "high"
		}

		// Technical tier labels
		if event.Entity.Labels != nil {
			if event.Entity.Labels["tier"] == "infrastructure" || event.Entity.Labels["tier"] == "system" {
				result.InfrastructureImpact *= 1.5
				result.TechnicalSeverity = "high"
			}
		}
	}

	// Cap values
	if result.InfrastructureImpact > 1.0 {
		result.InfrastructureImpact = 1.0
	}
	if result.CascadeRisk > 1.0 {
		result.CascadeRisk = 1.0
	}

	// Generate recommendations based on assessment
	ia.generateRecommendations(event, result)

	return result, nil
}

// assessKernelImpact evaluates kernel event impact
func (ia *ImpactAssessment) assessKernelImpact(kernel *domain.KernelData, result *ImpactResult) {
	// OOM kills are critical infrastructure events
	if kernel.Syscall == "oom_kill" || strings.Contains(kernel.Comm, "OOM") {
		result.TechnicalSeverity = "critical"
		result.InfrastructureImpact = 0.9
		result.RecommendedActions = append(result.RecommendedActions,
			"Increase memory limits for affected container",
			"Review application memory usage patterns",
			"Enable memory usage alerts",
		)
	}

	// Failed syscalls indicate issues
	if kernel.ReturnCode < 0 {
		switch kernel.Syscall {
		case "open", "openat":
			result.RecommendedActions = append(result.RecommendedActions,
				"Check file permissions",
				"Verify file exists",
			)
		case "connect", "bind":
			result.TechnicalSeverity = "high"
			result.RecommendedActions = append(result.RecommendedActions,
				"Check network connectivity",
				"Verify service endpoints",
			)
		case "malloc", "mmap":
			result.TechnicalSeverity = "high"
			result.InfrastructureImpact = 0.8
			result.RecommendedActions = append(result.RecommendedActions,
				"Monitor memory usage",
				"Check for memory leaks",
			)
		}
	}
}

// assessNetworkImpact evaluates network event impact
func (ia *ImpactAssessment) assessNetworkImpact(network *domain.NetworkData, result *ImpactResult) {
	// HTTP/gRPC errors
	if network.StatusCode >= 500 {
		result.TechnicalSeverity = "high"
		result.InfrastructureImpact = 0.8
		result.RecommendedActions = append(result.RecommendedActions,
			"Check service health",
			"Review error logs",
			"Enable circuit breakers",
		)
	} else if network.StatusCode >= 400 {
		result.TechnicalSeverity = "medium"
		result.InfrastructureImpact = 0.5
		result.RecommendedActions = append(result.RecommendedActions,
			"Review client requests",
			"Check API documentation",
		)
	}

	// High latency
	if network.Latency > 5000000000 { // > 5 seconds
		result.TechnicalSeverity = "high"
		result.InfrastructureImpact = 0.7
		result.CascadeRisk += 0.2 // Slow services cause cascading timeouts
		result.RecommendedActions = append(result.RecommendedActions,
			"Investigate service performance",
			"Check database queries",
			"Review network path",
		)
	}

	// Connection failures
	if network.StatusCode == 0 && network.Protocol != "" {
		result.TechnicalSeverity = "critical"
		result.InfrastructureImpact = 0.9
		result.RecommendedActions = append(result.RecommendedActions,
			"Verify service is running",
			"Check network policies",
			"Review firewall rules",
		)
	}
}

// assessApplicationImpact evaluates application event impact
func (ia *ImpactAssessment) assessApplicationImpact(app *domain.ApplicationData, result *ImpactResult) {
	// Error levels
	switch app.Level {
	case "critical", "fatal":
		result.TechnicalSeverity = "critical"
		result.InfrastructureImpact = 0.9
		result.RecommendedActions = append(result.RecommendedActions,
			"Immediate investigation required",
			"Check for data corruption",
			"Prepare rollback if needed",
		)
	case "error":
		result.TechnicalSeverity = "high"
		result.InfrastructureImpact = 0.7
		result.RecommendedActions = append(result.RecommendedActions,
			"Review error details",
			"Check recent deployments",
		)
	case "warn", "warning":
		result.TechnicalSeverity = "medium"
		result.InfrastructureImpact = 0.5
	}

	// Technical error types
	if app.ErrorType != "" {
		switch {
		case strings.Contains(strings.ToLower(app.ErrorType), "database"):
			result.InfrastructureImpact *= 1.3
			result.CascadeRisk += 0.3
			result.RecommendedActions = append(result.RecommendedActions,
				"Check database connections",
				"Review query performance",
			)
		case strings.Contains(strings.ToLower(app.ErrorType), "connection"):
			result.InfrastructureImpact *= 1.2
			result.CascadeRisk += 0.2
			result.RecommendedActions = append(result.RecommendedActions,
				"Verify network connectivity",
				"Check service discovery",
			)
		case strings.Contains(strings.ToLower(app.ErrorType), "timeout"):
			result.CascadeRisk += 0.4
			result.RecommendedActions = append(result.RecommendedActions,
				"Review timeout configurations",
				"Check downstream service health",
			)
		}
	}
}

// assessKubernetesImpact evaluates Kubernetes event impact
func (ia *ImpactAssessment) assessKubernetesImpact(k8s *domain.KubernetesData, result *ImpactResult) {
	// Warning events
	if k8s.EventType == "Warning" {
		result.TechnicalSeverity = "high"

		switch k8s.Reason {
		case "BackOff", "CrashLoopBackOff":
			result.InfrastructureImpact = 0.8
			result.RecommendedActions = append(result.RecommendedActions,
				"Check container logs",
				"Review liveness/readiness probes",
				"Verify image availability",
			)
		case "OOMKilled":
			result.InfrastructureImpact = 0.9
			result.TechnicalSeverity = "critical"
			result.RecommendedActions = append(result.RecommendedActions,
				"Increase memory limits",
				"Profile memory usage",
				"Check for memory leaks",
			)
		case "FailedScheduling":
			result.InfrastructureImpact = 0.7
			result.RecommendedActions = append(result.RecommendedActions,
				"Check node resources",
				"Review pod requirements",
				"Scale cluster if needed",
			)
		case "FailedMount":
			result.InfrastructureImpact = 0.8
			result.RecommendedActions = append(result.RecommendedActions,
				"Verify volume availability",
				"Check storage class",
				"Review PVC status",
			)
		}
	}

	// Object types impact assessment
	switch k8s.ObjectKind {
	case "Node":
		result.InfrastructureImpact *= 2.0 // Nodes are critical infrastructure
		result.CascadeRisk += 0.5
	case "Deployment", "StatefulSet", "DaemonSet":
		result.InfrastructureImpact *= 1.2 // Workload controllers
	case "Service", "Ingress":
		result.InfrastructureImpact *= 1.3 // Network objects affect connectivity
		result.CascadeRisk += 0.2
	case "PersistentVolumeClaim", "PersistentVolume":
		result.InfrastructureImpact *= 1.4 // Storage issues are critical
	case "NetworkPolicy":
		result.InfrastructureImpact *= 1.2 // Network policies affect connectivity
		result.CascadeRisk += 0.1
	}
}

// generateRecommendations creates actionable recommendations
func (ia *ImpactAssessment) generateRecommendations(event *domain.UnifiedEvent, result *ImpactResult) {
	// Add general recommendations based on severity
	switch result.TechnicalSeverity {
	case "critical":
		if len(result.RecommendedActions) == 0 {
			result.RecommendedActions = append(result.RecommendedActions,
				"Trigger incident response",
				"Notify on-call engineer",
				"Prepare rollback plan",
			)
		}
	case "high":
		if len(result.RecommendedActions) == 0 {
			result.RecommendedActions = append(result.RecommendedActions,
				"Investigate within 15 minutes",
				"Check monitoring dashboards",
				"Review recent changes",
			)
		}
	}

	// Add correlation-based recommendations
	if event.Correlation != nil && len(event.Correlation.CausalChain) > 3 {
		result.RecommendedActions = append(result.RecommendedActions,
			"Review event correlation chain",
			"Identify root cause in causal chain",
		)
	}

	// Add cascade risk recommendations
	if result.CascadeRisk > 0.5 {
		result.RecommendedActions = append(result.RecommendedActions,
			"Monitor dependent services",
			"Prepare to scale affected services",
			"Enable circuit breakers",
		)
	}

	// Infrastructure-specific recommendations
	if event.Entity != nil && event.Entity.Type == "node" {
		result.RecommendedActions = append(result.RecommendedActions,
			"Check node health metrics",
			"Verify kubelet status",
			"Review node conditions",
		)
	}
}
