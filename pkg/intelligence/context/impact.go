package context

import (
	"strings"

	"github.com/yairfalse/tapio/pkg/domain"
)

// ImpactAnalyzer calculates business impact and severity for events
type ImpactAnalyzer struct {
	// Business rules for determining impact
	criticalNamespaces   map[string]bool
	revenueNamespaces    map[string]bool
	customerServices     map[string]bool
	sloMonitoredServices map[string]bool

	// Severity weights based on event characteristics
	severityWeights map[string]float64

	// Thresholds for severity determination
	criticalThreshold float64
	highThreshold     float64
	mediumThreshold   float64
}

// NewImpactAnalyzer creates a new impact analyzer with default configuration
func NewImpactAnalyzer() *ImpactAnalyzer {
	return &ImpactAnalyzer{
		criticalNamespaces: map[string]bool{
			"production":     true,
			"prod":           true,
			"payments":       true,
			"billing":        true,
			"checkout":       true,
			"authentication": true,
			"auth":           true,
		},
		revenueNamespaces: map[string]bool{
			"payments":  true,
			"billing":   true,
			"checkout":  true,
			"orders":    true,
			"inventory": true,
			"cart":      true,
		},
		customerServices: map[string]bool{
			"api-gateway":     true,
			"frontend":        true,
			"mobile-api":      true,
			"web-api":         true,
			"graphql":         true,
			"checkout":        true,
			"user-service":    true,
			"cart-service":    true,
			"order-service":   true,
			"payment-service": true,
		},
		sloMonitoredServices: map[string]bool{
			"api-gateway":     true,
			"payment-service": true,
			"order-service":   true,
			"checkout":        true,
			"auth-service":    true,
		},
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

// NewImpactAnalyzerWithConfig creates an analyzer with custom configuration
func NewImpactAnalyzerWithConfig(
	criticalNamespaces map[string]bool,
	revenueNamespaces map[string]bool,
	customerServices map[string]bool,
	sloServices map[string]bool,
) *ImpactAnalyzer {
	analyzer := NewImpactAnalyzer()

	if criticalNamespaces != nil {
		analyzer.criticalNamespaces = criticalNamespaces
	}
	if revenueNamespaces != nil {
		analyzer.revenueNamespaces = revenueNamespaces
	}
	if customerServices != nil {
		analyzer.customerServices = customerServices
	}
	if sloServices != nil {
		analyzer.sloMonitoredServices = sloServices
	}

	return analyzer
}

// AssessImpact analyzes the business impact of an event
func (ia *ImpactAnalyzer) AssessImpact(ue *domain.UnifiedEvent) *domain.ImpactContext {
	if ue == nil {
		return nil
	}

	impact := &domain.ImpactContext{
		AffectedServices: ia.identifyAffectedServices(ue),
		CustomerFacing:   ia.isCustomerFacing(ue),
		RevenueImpacting: ia.isRevenueImpacting(ue),
		SLOImpact:        ia.hasSLOImpact(ue),
	}

	// Calculate business impact score
	impact.BusinessImpact = ia.calculateBusinessImpact(ue, impact)

	// Determine severity based on impact score and event characteristics
	impact.Severity = ia.determineSeverity(ue, impact.BusinessImpact)

	// Estimate affected users
	impact.AffectedUsers = ia.estimateAffectedUsers(ue, impact)

	return impact
}

// calculateBusinessImpact computes a normalized impact score (0.0-1.0)
func (ia *ImpactAnalyzer) calculateBusinessImpact(ue *domain.UnifiedEvent, impact *domain.ImpactContext) float64 {
	score := 0.0

	// Base score from event type and characteristics
	baseScore := ia.getBaseScore(ue)
	score += baseScore * 0.3

	// Namespace criticality
	if ia.isInCriticalNamespace(ue) {
		score += 0.2
	}

	// Customer-facing impact
	if impact.CustomerFacing {
		score += 0.2
	}

	// Revenue impact
	if impact.RevenueImpacting {
		score += 0.15
	}

	// SLO impact
	if impact.SLOImpact {
		score += 0.15
	}

	// Cap at 1.0
	if score > 1.0 {
		score = 1.0
	}

	return score
}

// determineSeverity maps impact score and event characteristics to severity levels
func (ia *ImpactAnalyzer) determineSeverity(ue *domain.UnifiedEvent, businessImpact float64) string {
	// Check for critical indicators
	if ia.hasCriticalIndicators(ue) || businessImpact >= ia.criticalThreshold {
		return "critical"
	}

	// Check for high severity indicators
	if ia.hasHighSeverityIndicators(ue) || businessImpact >= ia.highThreshold {
		return "high"
	}

	// Check for medium severity
	if businessImpact >= ia.mediumThreshold {
		return "medium"
	}

	return "low"
}

// isCustomerFacing checks if the event affects customer-facing services
func (ia *ImpactAnalyzer) isCustomerFacing(ue *domain.UnifiedEvent) bool {
	// Check entity context
	if ue.Entity != nil {
		serviceName := strings.ToLower(ue.Entity.Name)
		if ia.customerServices[serviceName] {
			return true
		}

		// Check if entity name contains customer-facing keywords
		for service := range ia.customerServices {
			if strings.Contains(serviceName, service) {
				return true
			}
		}

		// Check namespace
		namespace := strings.ToLower(ue.Entity.Namespace)
		if strings.Contains(namespace, "frontend") || strings.Contains(namespace, "api") {
			return true
		}
	}

	// Check Kubernetes data
	if ue.Kubernetes != nil && ue.Kubernetes.Object != "" {
		objectLower := strings.ToLower(ue.Kubernetes.Object)
		for service := range ia.customerServices {
			if strings.Contains(objectLower, service) {
				return true
			}
		}
	}

	// Check application logs for customer-facing indicators
	if ue.Application != nil && ue.Application.Message != "" {
		message := strings.ToLower(ue.Application.Message)
		customerIndicators := []string{"user", "customer", "client", "frontend", "ui", "mobile"}
		for _, indicator := range customerIndicators {
			if strings.Contains(message, indicator) {
				return true
			}
		}
	}

	return false
}

// isRevenueImpacting checks if the event affects revenue-critical services
func (ia *ImpactAnalyzer) isRevenueImpacting(ue *domain.UnifiedEvent) bool {
	// Check namespace
	if ue.Entity != nil {
		namespace := strings.ToLower(ue.Entity.Namespace)
		if ia.revenueNamespaces[namespace] {
			return true
		}

		// Check service name
		serviceName := strings.ToLower(ue.Entity.Name)
		revenueServices := []string{"payment", "billing", "checkout", "order", "cart", "subscription"}
		for _, service := range revenueServices {
			if strings.Contains(serviceName, service) {
				return true
			}
		}
	}

	// Check for payment/billing related errors
	if ue.Application != nil && ue.Application.Level == "error" {
		message := strings.ToLower(ue.Application.Message)
		revenueIndicators := []string{"payment", "transaction", "billing", "invoice", "charge", "refund"}
		for _, indicator := range revenueIndicators {
			if strings.Contains(message, indicator) {
				return true
			}
		}
	}

	return false
}

// hasSLOImpact checks if the event affects SLO-monitored services
func (ia *ImpactAnalyzer) hasSLOImpact(ue *domain.UnifiedEvent) bool {
	// Don't apply SLO impact to non-production environments
	if ue.Entity != nil {
		namespace := strings.ToLower(ue.Entity.Namespace)
		if strings.Contains(namespace, "staging") || strings.Contains(namespace, "test") || strings.Contains(namespace, "dev") {
			return false
		}

		serviceName := strings.ToLower(ue.Entity.Name)
		if ia.sloMonitoredServices[serviceName] {
			return true
		}

		// Check if entity name contains any SLO monitored service
		for sloService := range ia.sloMonitoredServices {
			if strings.Contains(serviceName, sloService) {
				return true
			}
		}
	}

	// Check if it's a latency or availability issue in production
	if ue.Type == domain.EventTypeNetwork || ue.Type == domain.EventTypeService {
		// Skip non-production environments
		if ue.Entity != nil {
			namespace := strings.ToLower(ue.Entity.Namespace)
			if strings.Contains(namespace, "staging") || strings.Contains(namespace, "test") || strings.Contains(namespace, "dev") {
				return false
			}
		}

		// Network timeouts or service failures impact SLOs
		if ue.Application != nil && ue.Application.Level == "error" {
			message := strings.ToLower(ue.Application.Message)
			sloIndicators := []string{"timeout", "unavailable", "refused", "latency", "slow"}
			for _, indicator := range sloIndicators {
				if strings.Contains(message, indicator) {
					return true
				}
			}
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

	// From network connections
	if ue.Network != nil {
		// Could identify services by port conventions
		if service := ia.identifyServiceByPort(ue.Network.DestPort); service != "" {
			services[service] = true
		}
	}

	// Convert map to slice
	result := make([]string, 0, len(services))
	for service := range services {
		result = append(result, service)
	}

	return result
}

// estimateAffectedUsers estimates the number of users impacted
func (ia *ImpactAnalyzer) estimateAffectedUsers(ue *domain.UnifiedEvent, impact *domain.ImpactContext) int {
	// Base estimation on service type and severity
	baseUsers := 0

	if impact.CustomerFacing {
		switch impact.Severity {
		case "critical":
			baseUsers = 10000 // All users potentially affected
		case "high":
			baseUsers = 5000 // Significant portion
		case "medium":
			baseUsers = 1000 // Moderate impact
		case "low":
			baseUsers = 100 // Limited impact
		}
	} else {
		// Internal services have indirect impact
		switch impact.Severity {
		case "critical":
			baseUsers = 1000
		case "high":
			baseUsers = 500
		case "medium":
			baseUsers = 100
		case "low":
			baseUsers = 10
		}
	}

	// Adjust based on namespace/service
	if ue.Entity != nil {
		namespace := strings.ToLower(ue.Entity.Namespace)
		if namespace == "production" || namespace == "prod" {
			baseUsers = int(float64(baseUsers) * 1.5)
		} else if strings.Contains(namespace, "staging") || strings.Contains(namespace, "test") {
			baseUsers = int(float64(baseUsers) * 0.1)
		}
	}

	return baseUsers
}

// Helper methods

func (ia *ImpactAnalyzer) getBaseScore(ue *domain.UnifiedEvent) float64 {
	// Base score by event type
	switch ue.Type {
	case domain.EventTypeSystem, domain.EventTypeCPU, domain.EventTypeMemory:
		// System issues have high base impact
		return 0.7
	case domain.EventTypeNetwork:
		// Network issues often cascade
		return 0.8
	case domain.EventTypeService:
		// Service failures are critical
		return 0.9
	case domain.EventTypeKubernetes:
		// Depends on the specific event
		if ue.Kubernetes != nil && ue.Kubernetes.EventType == "Warning" {
			return 0.4
		}
		return 0.4
	case domain.EventTypeLog:
		// Depends on log level
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

func (ia *ImpactAnalyzer) isInCriticalNamespace(ue *domain.UnifiedEvent) bool {
	if ue.Entity != nil {
		namespace := strings.ToLower(ue.Entity.Namespace)
		return ia.criticalNamespaces[namespace]
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

	// Check for critical service failures
	if ue.Type == domain.EventTypeService && ue.Application != nil {
		message := strings.ToLower(ue.Application.Message)
		criticalPatterns := []string{"panic", "fatal", "crash", "oom", "out of memory", "segfault"}
		for _, pattern := range criticalPatterns {
			if strings.Contains(message, pattern) {
				return true
			}
		}
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
			highPatterns := []string{"timeout", "refused", "unavailable", "degraded", "slow"}
			for _, pattern := range highPatterns {
				if strings.Contains(message, pattern) {
					return true
				}
			}
		}
	}

	// Kubernetes warnings are medium severity, not high
	// Remove this check from high severity indicators

	return false
}

func (ia *ImpactAnalyzer) identifyServiceByPort(port uint16) string {
	// Common service port mappings
	switch port {
	case 80, 8080:
		return "web-service"
	case 443, 8443:
		return "https-service"
	case 3306:
		return "mysql"
	case 5432:
		return "postgresql"
	case 6379:
		return "redis"
	case 9200:
		return "elasticsearch"
	case 5672:
		return "rabbitmq"
	case 9092:
		return "kafka"
	default:
		return ""
	}
}

// extractServiceName extracts service name from pod name or similar
func extractServiceName(podName string) string {
	// Remove common suffixes and hash
	// e.g., "payment-service-7d4b8c6f5-x2p4n" -> "payment-service"
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
	// Simple heuristic: contains both letters and numbers, length 5-10, no special chars
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
		} else {
			// Has special character, not a valid hash
			return false
		}
	}

	return hasLetter && hasNumber
}

// Configuration methods

// AddCriticalNamespace adds a namespace to the critical list
func (ia *ImpactAnalyzer) AddCriticalNamespace(namespace string) {
	ia.criticalNamespaces[strings.ToLower(namespace)] = true
}

// AddRevenueNamespace adds a namespace to the revenue-critical list
func (ia *ImpactAnalyzer) AddRevenueNamespace(namespace string) {
	ia.revenueNamespaces[strings.ToLower(namespace)] = true
}

// AddCustomerService adds a service to the customer-facing list
func (ia *ImpactAnalyzer) AddCustomerService(service string) {
	ia.customerServices[strings.ToLower(service)] = true
}

// AddSLOService adds a service to the SLO-monitored list
func (ia *ImpactAnalyzer) AddSLOService(service string) {
	ia.sloMonitoredServices[strings.ToLower(service)] = true
}

// SetSeverityThresholds updates the thresholds for severity determination
func (ia *ImpactAnalyzer) SetSeverityThresholds(critical, high, medium float64) {
	ia.criticalThreshold = critical
	ia.highThreshold = high
	ia.mediumThreshold = medium
}
