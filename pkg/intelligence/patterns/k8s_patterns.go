package patterns

import (
	"fmt"
	"strings"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// K8sPattern represents a behavioral pattern in Kubernetes
type K8sPattern struct {
	ID           string
	Name         string
	Category     PatternCategory
	Description  string
	Indicators   []PatternIndicator
	Impact       PatternImpact
	Correlations []string // Related pattern IDs
	RootCause    *RootCausePattern
}

// PatternCategory categorizes K8s behavioral patterns
type PatternCategory string

const (
	CategoryDeployment  PatternCategory = "deployment"
	CategoryScaling     PatternCategory = "scaling"
	CategoryFailure     PatternCategory = "failure"
	CategoryNetwork     PatternCategory = "network"
	CategoryResource    PatternCategory = "resource"
	CategorySecurity    PatternCategory = "security"
	CategoryPerformance PatternCategory = "performance"
)

// PatternIndicator defines what to look for
type PatternIndicator struct {
	Type       IndicatorType
	Field      string
	Condition  string
	Value      interface{}
	TimeWindow time.Duration
	Threshold  float64
}

// IndicatorType defines the type of pattern indicator
type IndicatorType string

const (
	IndicatorEvent     IndicatorType = "event"
	IndicatorMetric    IndicatorType = "metric"
	IndicatorState     IndicatorType = "state"
	IndicatorSequence  IndicatorType = "sequence"
	IndicatorFrequency IndicatorType = "frequency"
	IndicatorCausality IndicatorType = "causality"
)

// PatternImpact describes the impact of a pattern
type PatternImpact struct {
	Severity        string
	Scope           string // pod, service, namespace, cluster
	UserImpact      bool
	DataRisk        bool
	PerformanceRisk bool
}

// RootCausePattern represents root cause information for a pattern
type RootCausePattern struct {
	EventType   string
	Indicators  []string
	Probability float64
}

// K8sPatternLibrary contains all known K8s patterns
type K8sPatternLibrary struct {
	patterns   map[string]*K8sPattern
	byCategory map[PatternCategory][]*K8sPattern
}

// NewK8sPatternLibrary creates a new empty pattern library
func NewK8sPatternLibrary() *K8sPatternLibrary {
	return &K8sPatternLibrary{
		patterns:   make(map[string]*K8sPattern),
		byCategory: make(map[PatternCategory][]*K8sPattern),
	}
}

// initializePatterns loads all known K8s behavioral patterns
func (l *K8sPatternLibrary) initializePatterns() {
	// Deployment Patterns
	l.addPattern(&K8sPattern{
		ID:          "rolling-update-failure",
		Name:        "Rolling Update Failure",
		Category:    CategoryDeployment,
		Description: "New pods failing during rolling update",
		Indicators: []PatternIndicator{
			{
				Type:       IndicatorEvent,
				Field:      "reason",
				Condition:  "equals",
				Value:      "FailedScheduling",
				TimeWindow: 5 * time.Minute,
			},
			{
				Type:       IndicatorEvent,
				Field:      "reason",
				Condition:  "equals",
				Value:      "CrashLoopBackOff",
				TimeWindow: 5 * time.Minute,
			},
			{
				Type:       IndicatorState,
				Field:      "deployment.status.updatedReplicas",
				Condition:  "less_than",
				Value:      "deployment.spec.replicas",
				TimeWindow: 10 * time.Minute,
			},
		},
		Impact: PatternImpact{
			Severity:   "high",
			Scope:      "service",
			UserImpact: true,
		},
	})

	l.addPattern(&K8sPattern{
		ID:          "pod-eviction-storm",
		Name:        "Pod Eviction Storm",
		Category:    CategoryResource,
		Description: "Multiple pods being evicted due to resource pressure",
		Indicators: []PatternIndicator{
			{
				Type:       IndicatorFrequency,
				Field:      "reason:Evicted",
				Threshold:  5, // 5 evictions
				TimeWindow: 1 * time.Minute,
			},
			{
				Type:      IndicatorEvent,
				Field:     "message",
				Condition: "contains",
				Value:     "disk pressure",
			},
		},
		Impact: PatternImpact{
			Severity:        "critical",
			Scope:           "node",
			UserImpact:      true,
			PerformanceRisk: true,
		},
		Correlations: []string{"node-resource-exhaustion"},
	})

	// Scaling Patterns
	l.addPattern(&K8sPattern{
		ID:          "hpa-thrashing",
		Name:        "HPA Thrashing",
		Category:    CategoryScaling,
		Description: "HorizontalPodAutoscaler rapidly scaling up and down",
		Indicators: []PatternIndicator{
			{
				Type:       IndicatorSequence,
				Field:      "hpa.scale.direction",
				Condition:  "alternating",
				TimeWindow: 5 * time.Minute,
				Threshold:  3, // 3 alternations
			},
		},
		Impact: PatternImpact{
			Severity:        "medium",
			Scope:           "service",
			PerformanceRisk: true,
		},
	})

	// Network Patterns
	l.addPattern(&K8sPattern{
		ID:          "service-discovery-failure",
		Name:        "Service Discovery Failure",
		Category:    CategoryNetwork,
		Description: "Pods unable to resolve service DNS names",
		Indicators: []PatternIndicator{
			{
				Type:      IndicatorEvent,
				Field:     "dns.error",
				Condition: "contains",
				Value:     "NXDOMAIN",
			},
			{
				Type:      IndicatorEvent,
				Field:     "dns.query",
				Condition: "matches",
				Value:     "*.svc.cluster.local",
			},
		},
		Impact: PatternImpact{
			Severity:   "high",
			Scope:      "namespace",
			UserImpact: true,
		},
	})

	l.addPattern(&K8sPattern{
		ID:          "network-policy-blocking",
		Name:        "Network Policy Blocking Traffic",
		Category:    CategoryNetwork,
		Description: "Legitimate traffic blocked by network policies",
		Indicators: []PatternIndicator{
			{
				Type:       IndicatorEvent,
				Field:      "network.dropped",
				Condition:  "true",
				TimeWindow: 1 * time.Minute,
			},
			{
				Type:      IndicatorState,
				Field:     "network.tcp.state",
				Condition: "equals",
				Value:     "SYN_SENT",
			},
		},
		Impact: PatternImpact{
			Severity:   "high",
			Scope:      "pod",
			UserImpact: true,
		},
	})

	// Failure Patterns
	l.addPattern(&K8sPattern{
		ID:          "cascading-failure",
		Name:        "Cascading Service Failure",
		Category:    CategoryFailure,
		Description: "Failure propagating through dependent services",
		Indicators: []PatternIndicator{
			{
				Type:       IndicatorSequence,
				Field:      "service.error.upstream",
				TimeWindow: 2 * time.Minute,
			},
			{
				Type:      IndicatorMetric,
				Field:     "error_rate",
				Condition: "greater_than",
				Threshold: 0.5,
			},
		},
		Impact: PatternImpact{
			Severity:   "critical",
			Scope:      "namespace",
			UserImpact: true,
		},
	})

	l.addPattern(&K8sPattern{
		ID:          "init-container-failure",
		Name:        "Init Container Failure Pattern",
		Category:    CategoryFailure,
		Description: "Init containers repeatedly failing",
		Indicators: []PatternIndicator{
			{
				Type:      IndicatorState,
				Field:     "pod.status.initContainerStatuses.state",
				Condition: "equals",
				Value:     "terminated",
			},
			{
				Type:       IndicatorFrequency,
				Field:      "container.restart",
				Threshold:  3,
				TimeWindow: 5 * time.Minute,
			},
		},
		Impact: PatternImpact{
			Severity:   "high",
			Scope:      "pod",
			UserImpact: true,
		},
	})

	// Security Patterns
	l.addPattern(&K8sPattern{
		ID:          "privilege-escalation-attempt",
		Name:        "Privilege Escalation Attempt",
		Category:    CategorySecurity,
		Description: "Container attempting to gain elevated privileges",
		Indicators: []PatternIndicator{
			{
				Type:      IndicatorEvent,
				Field:     "syscall",
				Condition: "in",
				Value:     []string{"setuid", "setgid", "setns"},
			},
			{
				Type:      IndicatorState,
				Field:     "container.securityContext.privileged",
				Condition: "changed_to",
				Value:     true,
			},
		},
		Impact: PatternImpact{
			Severity: "critical",
			Scope:    "pod",
			DataRisk: true,
		},
	})

	l.addPattern(&K8sPattern{
		ID:          "suspicious-exec-pattern",
		Name:        "Suspicious Container Exec",
		Category:    CategorySecurity,
		Description: "Unusual commands executed in container",
		Indicators: []PatternIndicator{
			{
				Type:      IndicatorEvent,
				Field:     "exec.command",
				Condition: "contains",
				Value:     []string{"curl", "wget", "nc", "base64"},
			},
			{
				Type:      IndicatorEvent,
				Field:     "exec.path",
				Condition: "matches",
				Value:     "/proc/*/",
			},
		},
		Impact: PatternImpact{
			Severity: "high",
			Scope:    "pod",
			DataRisk: true,
		},
	})

	// Performance Patterns
	l.addPattern(&K8sPattern{
		ID:          "cpu-throttling",
		Name:        "CPU Throttling Pattern",
		Category:    CategoryPerformance,
		Description: "Containers being CPU throttled",
		Indicators: []PatternIndicator{
			{
				Type:      IndicatorMetric,
				Field:     "container.cpu.cfs_throttled_ratio",
				Condition: "greater_than",
				Threshold: 0.2, // 20% throttling
			},
			{
				Type:      IndicatorMetric,
				Field:     "container.cpu.usage",
				Condition: "equals",
				Value:     "container.cpu.limit",
			},
		},
		Impact: PatternImpact{
			Severity:        "medium",
			Scope:           "pod",
			PerformanceRisk: true,
			UserImpact:      true,
		},
	})

	l.addPattern(&K8sPattern{
		ID:          "memory-leak",
		Name:        "Memory Leak Pattern",
		Category:    CategoryPerformance,
		Description: "Container showing signs of memory leak",
		Indicators: []PatternIndicator{
			{
				Type:       IndicatorMetric,
				Field:      "container.memory.usage",
				Condition:  "increasing",
				TimeWindow: 1 * time.Hour,
			},
			{
				Type:       IndicatorFrequency,
				Field:      "oom_kill",
				Threshold:  2,
				TimeWindow: 1 * time.Hour,
			},
		},
		Impact: PatternImpact{
			Severity:        "high",
			Scope:           "pod",
			PerformanceRisk: true,
		},
	})

	// Resource Patterns
	l.addPattern(&K8sPattern{
		ID:          "node-resource-exhaustion",
		Name:        "Node Resource Exhaustion",
		Category:    CategoryResource,
		Description: "Node running out of resources",
		Indicators: []PatternIndicator{
			{
				Type:      IndicatorState,
				Field:     "node.status.conditions.DiskPressure",
				Condition: "equals",
				Value:     "True",
			},
			{
				Type:      IndicatorState,
				Field:     "node.status.conditions.MemoryPressure",
				Condition: "equals",
				Value:     "True",
			},
		},
		Impact: PatternImpact{
			Severity:   "critical",
			Scope:      "node",
			UserImpact: true,
		},
		Correlations: []string{"pod-eviction-storm"},
	})

	l.addPattern(&K8sPattern{
		ID:          "pvc-mount-failure",
		Name:        "PersistentVolume Mount Failure",
		Category:    CategoryResource,
		Description: "Pods unable to mount persistent volumes",
		Indicators: []PatternIndicator{
			{
				Type:      IndicatorEvent,
				Field:     "reason",
				Condition: "equals",
				Value:     "FailedMount",
			},
			{
				Type:      IndicatorEvent,
				Field:     "message",
				Condition: "contains",
				Value:     "volume",
			},
		},
		Impact: PatternImpact{
			Severity:   "high",
			Scope:      "pod",
			UserImpact: true,
			DataRisk:   true,
		},
	})
}

// addPattern adds a pattern to the library
func (l *K8sPatternLibrary) addPattern(pattern *K8sPattern) {
	l.patterns[pattern.ID] = pattern
	l.byCategory[pattern.Category] = append(l.byCategory[pattern.Category], pattern)
}

// GetPattern retrieves a pattern by ID
func (l *K8sPatternLibrary) GetPattern(id string) (*K8sPattern, bool) {
	pattern, exists := l.patterns[id]
	return pattern, exists
}

// GetPatternsByCategory retrieves all patterns in a category
func (l *K8sPatternLibrary) GetPatternsByCategory(category PatternCategory) []*K8sPattern {
	return l.byCategory[category]
}

// MatchEvent checks if an event matches any patterns
func (l *K8sPatternLibrary) MatchEvent(event *domain.UnifiedEvent) []*PatternMatch {
	var matches []*PatternMatch

	for _, pattern := range l.patterns {
		if match := l.checkPattern(event, pattern); match != nil {
			matches = append(matches, match)
		}
	}

	return matches
}

// PatternMatch represents a matched pattern
type PatternMatch struct {
	Pattern           *K8sPattern
	Confidence        float64
	MatchedIndicators []string
	Context           map[string]interface{}
}

// checkPattern checks if an event matches a pattern
func (l *K8sPatternLibrary) checkPattern(event *domain.UnifiedEvent, pattern *K8sPattern) *PatternMatch {
	matchedIndicators := []string{}
	totalIndicators := len(pattern.Indicators)

	for _, indicator := range pattern.Indicators {
		if l.checkIndicator(event, indicator) {
			matchedIndicators = append(matchedIndicators,
				fmt.Sprintf("%s.%s %s", indicator.Type, indicator.Field, indicator.Condition))
		}
	}

	if len(matchedIndicators) == 0 {
		return nil
	}

	confidence := float64(len(matchedIndicators)) / float64(totalIndicators)

	return &PatternMatch{
		Pattern:           pattern,
		Confidence:        confidence,
		MatchedIndicators: matchedIndicators,
		Context:           l.extractContext(event, pattern),
	}
}

// checkIndicator checks if an event matches a single indicator
func (l *K8sPatternLibrary) checkIndicator(event *domain.UnifiedEvent, indicator PatternIndicator) bool {
	switch indicator.Type {
	case IndicatorEvent:
		return l.checkEventIndicator(event, indicator)
	case IndicatorState:
		return l.checkStateIndicator(event, indicator)
	case IndicatorMetric:
		return l.checkMetricIndicator(event, indicator)
	default:
		return false
	}
}

// checkEventIndicator checks event-based indicators
func (l *K8sPatternLibrary) checkEventIndicator(event *domain.UnifiedEvent, indicator PatternIndicator) bool {
	// Check Kubernetes events
	if event.Kubernetes != nil {
		switch indicator.Field {
		case "reason":
			return l.checkCondition(event.Kubernetes.Reason, indicator.Condition, indicator.Value)
		case "message":
			return l.checkCondition(event.Kubernetes.Message, indicator.Condition, indicator.Value)
		}
	}

	// Check network events
	if event.Network != nil && strings.HasPrefix(indicator.Field, "network.") {
		switch indicator.Field {
		case "network.dropped":
			// Would check if packet was dropped
			return false
		}
	}

	// Check kernel events
	if event.Kernel != nil && indicator.Field == "syscall" {
		return l.checkCondition(event.Kernel.Syscall, indicator.Condition, indicator.Value)
	}

	return false
}

// checkStateIndicator checks state-based indicators
func (l *K8sPatternLibrary) checkStateIndicator(event *domain.UnifiedEvent, indicator PatternIndicator) bool {
	// For now, check state through Kubernetes data
	if event.Kubernetes != nil {
		// Check state-related fields in Kubernetes data
		if indicator.Field == "deployment.status.updatedReplicas" {
			// This would need actual deployment status
			return false
		}
	}

	// Check state info if available
	if event.State != nil {
		if indicator.Field == "state.current" {
			return l.checkCondition(event.State.Current, indicator.Condition, indicator.Value)
		}
		if indicator.Field == "state.transition" {
			return l.checkCondition(event.State.Transition, indicator.Condition, indicator.Value)
		}
	}

	return false
}

// checkMetricIndicator checks metric-based indicators
func (l *K8sPatternLibrary) checkMetricIndicator(event *domain.UnifiedEvent, indicator PatternIndicator) bool {
	// This would check metrics data
	// For now, return false as we don't have metrics in the event
	return false
}

// checkCondition evaluates a condition
func (l *K8sPatternLibrary) checkCondition(actual interface{}, condition string, expected interface{}) bool {
	switch condition {
	case "equals":
		return fmt.Sprintf("%v", actual) == fmt.Sprintf("%v", expected)
	case "contains":
		return strings.Contains(fmt.Sprintf("%v", actual), fmt.Sprintf("%v", expected))
	case "matches":
		// Would use regex matching
		return strings.Contains(fmt.Sprintf("%v", actual), fmt.Sprintf("%v", expected))
	case "greater_than":
		// Would do numeric comparison
		return false
	case "in":
		// Check if actual is in expected slice
		if values, ok := expected.([]string); ok {
			actualStr := fmt.Sprintf("%v", actual)
			for _, v := range values {
				if actualStr == v {
					return true
				}
			}
		}
		return false
	default:
		return false
	}
}

// extractContext extracts relevant context for a pattern match
func (l *K8sPatternLibrary) extractContext(event *domain.UnifiedEvent, pattern *K8sPattern) map[string]interface{} {
	context := make(map[string]interface{})

	// Extract from Kubernetes data
	if event.Kubernetes != nil {
		if event.Kubernetes.Object != "" {
			context["object"] = event.Kubernetes.Object
			// Extract namespace and name from object path if present
			parts := strings.Split(event.Kubernetes.Object, "/")
			if len(parts) >= 2 {
				context["kind"] = parts[0]
				context["name"] = parts[1]
			}
		}
	}

	if event.Entity != nil {
		context["entity_type"] = event.Entity.Type
		context["entity_name"] = event.Entity.Name
		if event.Entity.Namespace != "" {
			context["namespace"] = event.Entity.Namespace
		}
	}

	context["timestamp"] = event.Timestamp
	context["severity"] = event.Severity

	return context
}

// GetRelatedPatterns finds patterns related to a given pattern
func (l *K8sPatternLibrary) GetRelatedPatterns(patternID string) []*K8sPattern {
	pattern, exists := l.patterns[patternID]
	if !exists {
		return nil
	}

	var related []*K8sPattern
	for _, relatedID := range pattern.Correlations {
		if relatedPattern, exists := l.patterns[relatedID]; exists {
			related = append(related, relatedPattern)
		}
	}

	return related
}
