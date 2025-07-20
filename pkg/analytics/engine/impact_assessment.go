package engine

import (
	"context"
	"strings"

	"github.com/yairfalse/tapio/pkg/domain"
)

// ImpactAssessment evaluates the business and technical impact of events
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
		TechnicalSeverity:  "medium", // default
		BusinessImpact:     0.5,      // default
		CascadeRisk:        0.0,
		AffectedServices:   []string{},
		RecommendedActions: []string{},
	}

	// Use event's impact context if available
	if event.Impact != nil {
		result.TechnicalSeverity = event.Impact.Severity
		result.BusinessImpact = event.Impact.BusinessImpact
		result.AffectedServices = event.Impact.AffectedServices

		// Increase cascade risk for customer-facing or revenue-impacting events
		if event.Impact.CustomerFacing {
			result.CascadeRisk += 0.3
		}
		if event.Impact.RevenueImpacting {
			result.CascadeRisk += 0.4
		}
		if event.Impact.SLOImpact {
			result.CascadeRisk += 0.3
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

	// Adjust severity based on entity type
	if event.Entity != nil {
		switch event.Entity.Type {
		case "database":
			result.BusinessImpact *= 1.5 // Databases are critical
			result.CascadeRisk += 0.2
		case "gateway", "loadbalancer":
			result.BusinessImpact *= 1.3 // Gateways affect many services
			result.CascadeRisk += 0.3
		case "cache":
			result.BusinessImpact *= 1.1 // Cache issues cause performance degradation
		}

		// Critical namespace/labels
		if event.Entity.Namespace == "production" || event.Entity.Namespace == "prod" {
			result.BusinessImpact *= 1.2
		}
		if event.Entity.Labels != nil {
			if event.Entity.Labels["tier"] == "critical" || event.Entity.Labels["tier"] == "tier-1" {
				result.BusinessImpact *= 1.5
				result.TechnicalSeverity = "critical"
			}
		}
	}

	// Cap values
	if result.BusinessImpact > 1.0 {
		result.BusinessImpact = 1.0
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
	// OOM kills are critical
	if kernel.Syscall == "oom_kill" || strings.Contains(kernel.Comm, "OOM") {
		result.TechnicalSeverity = "critical"
		result.BusinessImpact = 0.9
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
			result.BusinessImpact = 0.8
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
		result.BusinessImpact = 0.8
		result.RecommendedActions = append(result.RecommendedActions,
			"Check service health",
			"Review error logs",
			"Enable circuit breakers",
		)
	} else if network.StatusCode >= 400 {
		result.TechnicalSeverity = "medium"
		result.BusinessImpact = 0.6
		result.RecommendedActions = append(result.RecommendedActions,
			"Review client requests",
			"Check API documentation",
		)
	}

	// High latency
	if network.Latency > 5000000000 { // > 5 seconds
		result.TechnicalSeverity = "high"
		result.BusinessImpact = 0.7
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
		result.BusinessImpact = 0.9
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
		result.BusinessImpact = 0.9
		result.RecommendedActions = append(result.RecommendedActions,
			"Immediate investigation required",
			"Check for data corruption",
			"Prepare rollback if needed",
		)
	case "error":
		result.TechnicalSeverity = "high"
		result.BusinessImpact = 0.7
		result.RecommendedActions = append(result.RecommendedActions,
			"Review error details",
			"Check recent deployments",
		)
	case "warn", "warning":
		result.TechnicalSeverity = "medium"
		result.BusinessImpact = 0.5
	}

	// Specific error types
	if app.ErrorType != "" {
		switch {
		case strings.Contains(strings.ToLower(app.ErrorType), "database"):
			result.BusinessImpact *= 1.3
			result.CascadeRisk += 0.3
			result.RecommendedActions = append(result.RecommendedActions,
				"Check database connections",
				"Review query performance",
			)
		case strings.Contains(strings.ToLower(app.ErrorType), "auth"):
			result.BusinessImpact *= 1.2
			result.RecommendedActions = append(result.RecommendedActions,
				"Verify authentication service",
				"Check token validity",
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
			result.BusinessImpact = 0.8
			result.RecommendedActions = append(result.RecommendedActions,
				"Check container logs",
				"Review liveness/readiness probes",
				"Verify image availability",
			)
		case "OOMKilled":
			result.BusinessImpact = 0.9
			result.TechnicalSeverity = "critical"
			result.RecommendedActions = append(result.RecommendedActions,
				"Increase memory limits",
				"Profile memory usage",
				"Check for memory leaks",
			)
		case "FailedScheduling":
			result.BusinessImpact = 0.7
			result.RecommendedActions = append(result.RecommendedActions,
				"Check node resources",
				"Review pod requirements",
				"Scale cluster if needed",
			)
		case "FailedMount":
			result.BusinessImpact = 0.8
			result.RecommendedActions = append(result.RecommendedActions,
				"Verify volume availability",
				"Check storage class",
				"Review PVC status",
			)
		}
	}

	// Object types
	switch k8s.ObjectKind {
	case "Deployment", "StatefulSet", "DaemonSet":
		result.BusinessImpact *= 1.2 // Workload controllers are important
	case "Service", "Ingress":
		result.BusinessImpact *= 1.3 // Network objects affect connectivity
		result.CascadeRisk += 0.2
	case "PersistentVolumeClaim", "PersistentVolume":
		result.BusinessImpact *= 1.4 // Storage issues are critical
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
}
