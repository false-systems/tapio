package correlation

import (
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// CorrelationPattern defines a pattern to match in event sequences
type CorrelationPattern interface {
	Match(events []*domain.UnifiedEvent) *PatternMatch
	Name() string
}

// PatternMatch represents a matched pattern
type PatternMatch struct {
	Pattern    string
	Confidence float64
	RootCause  *domain.UnifiedEvent
	Events     []*domain.UnifiedEvent
	Evidence   []string
}

// CascadeFailurePattern detects cascading failures
type CascadeFailurePattern struct{}

func (p *CascadeFailurePattern) Name() string {
	return "cascade_failure"
}

func (p *CascadeFailurePattern) Match(events []*domain.UnifiedEvent) *PatternMatch {
	// Look for OOM kill followed by pod failures
	var oomEvent *domain.UnifiedEvent
	cascadeEvents := []*domain.UnifiedEvent{}

	for _, event := range events {
		// Find OOM kill
		if event.Kernel != nil && event.Kernel.Syscall == "oom_kill" {
			oomEvent = event
			continue
		}

		// Find subsequent pod failures
		if oomEvent != nil && event.Kubernetes != nil {
			timeDiff := event.Timestamp.Sub(oomEvent.Timestamp)
			if timeDiff > 0 && timeDiff < 5*time.Minute {
				if event.Kubernetes.Reason == "OOMKilled" ||
					event.Kubernetes.Reason == "BackOff" ||
					event.Kubernetes.Reason == "CrashLoopBackOff" {
					cascadeEvents = append(cascadeEvents, event)
				}
			}
		}
	}

	if oomEvent != nil && len(cascadeEvents) > 0 {
		return &PatternMatch{
			Pattern:    "cascade_failure",
			Confidence: 0.95,
			RootCause:  oomEvent,
			Events:     cascadeEvents,
			Evidence: []string{
				fmt.Sprintf("OOM kill at %s", oomEvent.Timestamp.Format(time.RFC3339)),
				fmt.Sprintf("Followed by %d pod failures", len(cascadeEvents)),
			},
		}
	}

	return nil
}

// DeploymentFailurePattern detects deployment-related failures
type DeploymentFailurePattern struct{}

func (p *DeploymentFailurePattern) Name() string {
	return "deployment_failure"
}

func (p *DeploymentFailurePattern) Match(events []*domain.UnifiedEvent) *PatternMatch {
	// Look for deployment update followed by errors
	var deploymentEvent *domain.UnifiedEvent
	errorEvents := []*domain.UnifiedEvent{}

	for _, event := range events {
		// Find deployment update
		if event.Kubernetes != nil && event.Kubernetes.ObjectKind == "Deployment" {
			if event.Kubernetes.Action == "MODIFIED" {
				deploymentEvent = event
				continue
			}
		}

		// Find subsequent errors
		if deploymentEvent != nil {
			timeDiff := event.Timestamp.Sub(deploymentEvent.Timestamp)
			if timeDiff > 0 && timeDiff < 10*time.Minute {
				if event.Severity == domain.EventSeverityError || event.Severity == domain.EventSeverityCritical {
					errorEvents = append(errorEvents, event)
				}
			}
		}
	}

	if deploymentEvent != nil && len(errorEvents) >= 3 {
		return &PatternMatch{
			Pattern:    "deployment_failure",
			Confidence: 0.85,
			RootCause:  deploymentEvent,
			Events:     errorEvents,
			Evidence: []string{
				"Deployment update detected",
				fmt.Sprintf("%d errors within 10 minutes", len(errorEvents)),
			},
		}
	}

	return nil
}

// NetworkStormPattern detects network-related issues
type NetworkStormPattern struct{}

func (p *NetworkStormPattern) Name() string {
	return "network_storm"
}

func (p *NetworkStormPattern) Match(events []*domain.UnifiedEvent) *PatternMatch {
	// Look for multiple network errors in short time
	networkErrors := []*domain.UnifiedEvent{}
	startTime := time.Time{}

	for _, event := range events {
		if event.Network != nil && event.Network.StatusCode >= 500 {
			if startTime.IsZero() {
				startTime = event.Timestamp
			}

			// Within 30 seconds of first error
			if event.Timestamp.Sub(startTime) < 30*time.Second {
				networkErrors = append(networkErrors, event)
			}
		}
	}

	if len(networkErrors) >= 5 {
		return &PatternMatch{
			Pattern:    "network_storm",
			Confidence: 0.9,
			RootCause:  networkErrors[0],
			Events:     networkErrors[1:],
			Evidence: []string{
				fmt.Sprintf("%d network errors in 30 seconds", len(networkErrors)),
				"Multiple services affected",
			},
		}
	}

	return nil
}

// ResourceExhaustionPattern detects resource exhaustion
type ResourceExhaustionPattern struct{}

func (p *ResourceExhaustionPattern) Name() string {
	return "resource_exhaustion"
}

func (p *ResourceExhaustionPattern) Match(events []*domain.UnifiedEvent) *PatternMatch {
	// Look for resource limit events
	var resourceEvent *domain.UnifiedEvent
	impactedEvents := []*domain.UnifiedEvent{}

	for _, event := range events {
		// CPU or memory limit reached
		if event.Metrics != nil {
			if (event.Metrics.MetricName == "cpu_usage_percent" && event.Metrics.Value > 95) ||
				(event.Metrics.MetricName == "memory_usage_percent" && event.Metrics.Value > 95) {
				resourceEvent = event
			}
		}

		// Find impacted events
		if resourceEvent != nil {
			timeDiff := event.Timestamp.Sub(resourceEvent.Timestamp)
			if timeDiff > 0 && timeDiff < 2*time.Minute {
				if event.Severity >= domain.EventSeverityWarning {
					impactedEvents = append(impactedEvents, event)
				}
			}
		}
	}

	if resourceEvent != nil && len(impactedEvents) > 0 {
		return &PatternMatch{
			Pattern:    "resource_exhaustion",
			Confidence: 0.88,
			RootCause:  resourceEvent,
			Events:     impactedEvents,
			Evidence: []string{
				"Resource limits reached",
				fmt.Sprintf("%d subsequent errors", len(impactedEvents)),
			},
		}
	}

	return nil
}

// initializePatterns creates all available patterns
func initializePatterns() []CorrelationPattern {
	return []CorrelationPattern{
		&CascadeFailurePattern{},
		&DeploymentFailurePattern{},
		&NetworkStormPattern{},
		&ResourceExhaustionPattern{},
	}
}
