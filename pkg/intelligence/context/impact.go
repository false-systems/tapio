package context

import (
	"strings"

	"github.com/yairfalse/tapio/pkg/domain"
)

// ImpactAnalyzer calculates infrastructure impact and severity for events
type ImpactAnalyzer struct {
	// Infrastructure-based severity weights
	severityWeights map[string]float64

	// Thresholds for severity determination
	criticalThreshold float64
	highThreshold     float64
	mediumThreshold   float64
}

// NewImpactAnalyzer creates a new impact analyzer focused on infrastructure
func NewImpactAnalyzer() *ImpactAnalyzer {
	return &ImpactAnalyzer{
		severityWeights: map[string]float64{
			"error_rate":     0.25,
			"latency":        0.20,
			"availability":   0.30,
			"data_integrity": 0.25,
		},
		criticalThreshold: 0.8,
		highThreshold:     0.6,
		mediumThreshold:   0.3,
	}
}

// AssessImpact analyzes the infrastructure impact of an event
func (ia *ImpactAnalyzer) AssessImpact(ue *domain.UnifiedEvent) *domain.ImpactContext {
	if ue == nil {
		return nil
	}

	impact := &domain.ImpactContext{
		AffectedServices: ia.identifyAffectedServices(ue),
	}

	// Calculate infrastructure impact score based on technical indicators
	impactScore := ia.calculateInfrastructureImpact(ue)

	// Determine severity based on technical characteristics
	impact.Severity = ia.determineSeverity(ue, impactScore)

	// Set technical impact indicators (infrastructure only)
	impact.InfrastructureImpact = impactScore
	impact.SLOImpact = ia.hasSLOImpact(ue) // Keep as this is technical

	return impact
}

// calculateInfrastructureImpact computes a normalized impact score (0.0-1.0) based on technical indicators
func (ia *ImpactAnalyzer) calculateInfrastructureImpact(ue *domain.UnifiedEvent) float64 {
	score := 0.0

	// Base score from event type and technical characteristics
	baseScore := ia.getBaseScore(ue)
	score += baseScore * 0.4

	// Production environment detection (without business assumptions)
	if ia.isProductionEnvironment(ue) {
		score += 0.2
	}

	// Cascade risk based on resource type
	cascadeRisk := ia.calculateCascadeRisk(ue)
	score += cascadeRisk * 0.2

	// System resource criticality
	if ia.isSystemCritical(ue) {
		score += 0.2
	}

	// Cap at 1.0
	if score > 1.0 {
		score = 1.0
	}

	return score
}

// determineSeverity maps impact score and event characteristics to severity levels
func (ia *ImpactAnalyzer) determineSeverity(ue *domain.UnifiedEvent, impactScore float64) string {
	// Check for critical technical indicators
	if ia.hasCriticalIndicators(ue) || impactScore >= ia.criticalThreshold {
		return "critical"
	}

	// Check for high severity technical indicators
	if ia.hasHighSeverityIndicators(ue) || impactScore >= ia.highThreshold {
		return "high"
	}

	// Check for medium severity
	if impactScore >= ia.mediumThreshold {
		return "medium"
	}

	return "low"
}

// hasSLOImpact checks if the event affects service level objectives (technical metric)
func (ia *ImpactAnalyzer) hasSLOImpact(ue *domain.UnifiedEvent) bool {
	// Check for latency or availability issues
	if ue.Type == domain.EventTypeNetwork || ue.Type == domain.EventTypeService {
		// Network timeouts or service failures impact SLOs
		if ue.Application != nil && ue.Application.Level == "error" {
			message := strings.ToLower(ue.Application.Message)
			sloIndicators := []string{"timeout", "unavailable", "refused", "latency", "slow", "degraded"}
			for _, indicator := range sloIndicators {
				if strings.Contains(message, indicator) {
					return true
				}
			}
		}

		// High latency network events
		if ue.Network != nil && ue.Network.Latency > 1000000000 { // > 1 second
			return true
		}
	}

	return false
}

// identifyAffectedServices determines which services are impacted
func (ia *ImpactAnalyzer) identifyAffectedServices(ue *domain.UnifiedEvent) []string {
	services := make(map[string]bool)

	// From entity context
	if ue.Entity != nil && ue.Entity.Name != "" {
		services[ue.Entity.Name] = true
	}

	// From Kubernetes data
	if ue.Kubernetes != nil && ue.Kubernetes.Object != "" {
		// Extract service name from object (e.g., "Pod/service-name-xxx")
		parts := strings.Split(ue.Kubernetes.Object, "/")
		if len(parts) > 1 {
			serviceName := extractServiceName(parts[1])
			if serviceName != "" {
				services[serviceName] = true
			}
		}
	}

	// Convert map to slice
	result := make([]string, 0, len(services))
	for service := range services {
		result = append(result, service)
	}

	return result
}

// Infrastructure-focused helper methods

func (ia *ImpactAnalyzer) getBaseScore(ue *domain.UnifiedEvent) float64 {
	// Base score by event type (technical severity)
	switch ue.Type {
	case domain.EventTypeSystem, domain.EventTypeCPU, domain.EventTypeMemory:
		// System resource issues
		return 0.7
	case domain.EventTypeNetwork:
		// Network issues can cascade
		return 0.6
	case domain.EventTypeService:
		// Service failures
		return 0.8
	case domain.EventTypeKubernetes:
		// Kubernetes events
		if ue.Kubernetes != nil && ue.Kubernetes.EventType == "Warning" {
			return 0.5
		}
		return 0.4
	case domain.EventTypeLog:
		// Based on log level
		if ue.Application != nil {
			switch ue.Application.Level {
			case "error", "fatal":
				return 0.7
			case "warn":
				return 0.4
			default:
				return 0.2
			}
		}
		return 0.3
	default:
		return 0.5
	}
}

func (ia *ImpactAnalyzer) isProductionEnvironment(ue *domain.UnifiedEvent) bool {
	if ue.Entity != nil {
		namespace := strings.ToLower(ue.Entity.Namespace)
		// Simple check for non-development environments
		return !strings.Contains(namespace, "dev") &&
			!strings.Contains(namespace, "test") &&
			!strings.Contains(namespace, "staging")
	}
	return true // Assume production if unknown
}

func (ia *ImpactAnalyzer) calculateCascadeRisk(ue *domain.UnifiedEvent) float64 {
	risk := 0.0

	// Network components have high cascade risk
	if ue.Entity != nil {
		entityType := strings.ToLower(ue.Entity.Type)
		switch entityType {
		case "service", "ingress", "networkpolicy":
			risk = 0.8
		case "deployment", "replicaset", "statefulset":
			risk = 0.6
		case "pod":
			risk = 0.4
		case "configmap", "secret":
			risk = 0.3
		}
	}

	// Critical infrastructure components
	if ue.Kubernetes != nil {
		objKind := strings.ToLower(ue.Kubernetes.ObjectKind)
		if objKind == "node" {
			risk = 1.0 // Node issues affect all pods
		} else if objKind == "namespace" {
			risk = 0.9 // Namespace issues affect multiple resources
		}
	}

	return risk
}

func (ia *ImpactAnalyzer) isSystemCritical(ue *domain.UnifiedEvent) bool {
	// Check for system-level critical resources
	if ue.Entity != nil {
		namespace := strings.ToLower(ue.Entity.Namespace)
		// System namespaces are critical
		if namespace == "kube-system" || namespace == "kube-public" || namespace == "kube-node-lease" {
			return true
		}

		// Critical workload types
		entityName := strings.ToLower(ue.Entity.Name)
		criticalComponents := []string{"kube-apiserver", "kube-controller", "kube-scheduler", "etcd", "coredns", "kube-proxy"}
		for _, component := range criticalComponents {
			if strings.Contains(entityName, component) {
				return true
			}
		}
	}

	return false
}

func (ia *ImpactAnalyzer) hasCriticalIndicators(ue *domain.UnifiedEvent) bool {
	// Check for critical error patterns
	if ue.Application != nil && ue.Application.Level == "fatal" {
		return true
	}

	// Check for critical system events
	if ue.Type == domain.EventTypeSystem && ue.Kernel != nil {
		// OOM killer, kernel panic indicators
		if ue.Kernel.Comm == "oom_reaper" || ue.Kernel.Syscall == "panic" {
			return true
		}
	}

	// Check for critical failures
	if ue.Application != nil {
		message := strings.ToLower(ue.Application.Message)
		criticalPatterns := []string{"panic", "fatal", "crash", "oom", "out of memory", "segfault", "kernel panic"}
		for _, pattern := range criticalPatterns {
			if strings.Contains(message, pattern) {
				return true
			}
		}
	}

	// Node failures are critical
	if ue.Kubernetes != nil && ue.Kubernetes.ObjectKind == "Node" && ue.Kubernetes.Action == "DELETED" {
		return true
	}

	return false
}

func (ia *ImpactAnalyzer) hasHighSeverityIndicators(ue *domain.UnifiedEvent) bool {
	// Check for error conditions
	if ue.Application != nil && ue.Application.Level == "error" {
		return true
	}

	// Check for service degradation
	if ue.Type == domain.EventTypeService || ue.Type == domain.EventTypeNetwork {
		if ue.Application != nil {
			message := strings.ToLower(ue.Application.Message)
			highPatterns := []string{"timeout", "refused", "unavailable", "degraded", "slow", "connection reset"}
			for _, pattern := range highPatterns {
				if strings.Contains(message, pattern) {
					return true
				}
			}
		}
	}

	// Pod evictions and failures
	if ue.Kubernetes != nil {
		if ue.Kubernetes.Reason == "Evicted" || ue.Kubernetes.Reason == "Failed" || ue.Kubernetes.Reason == "OOMKilled" {
			return true
		}
	}

	return false
}

// extractServiceName extracts service name from pod name or similar
func extractServiceName(podName string) string {
	// Remove common suffixes and hash
	// e.g., "nginx-deployment-7d4b8c6f5-x2p4n" -> "nginx-deployment"
	parts := strings.Split(podName, "-")
	if len(parts) < 2 {
		return podName
	}

	// Remove last 2 parts if they look like deployment hash and pod id
	if len(parts) > 2 && isHash(parts[len(parts)-2]) && isHash(parts[len(parts)-1]) {
		return strings.Join(parts[:len(parts)-2], "-")
	}

	// Remove last part if it looks like pod id
	if isHash(parts[len(parts)-1]) {
		return strings.Join(parts[:len(parts)-1], "-")
	}

	return podName
}

func isHash(s string) bool {
	// Simple heuristic: contains both letters and numbers, length 5-10
	if len(s) < 5 || len(s) > 10 {
		return false
	}

	hasLetter := false
	hasNumber := false
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			hasLetter = true
		} else if r >= '0' && r <= '9' {
			hasNumber = true
		} else if r != '-' {
			// Allow hyphens in hashes
			return false
		}
	}

	return hasLetter && hasNumber
}

// Configuration methods

// SetSeverityThresholds updates the thresholds for severity determination
func (ia *ImpactAnalyzer) SetSeverityThresholds(critical, high, medium float64) {
	ia.criticalThreshold = critical
	ia.highThreshold = high
	ia.mediumThreshold = medium
}
