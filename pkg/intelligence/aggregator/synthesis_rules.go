package aggregator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"
)

// SynthesisRule defines a rule that combines multiple findings into a higher-level insight
type SynthesisRule struct {
	ID          string
	Name        string
	Description string
	Priority    int
	// Conditions that must be met for this rule to apply
	Preconditions []SynthesisPrecondition
	// Synthesis function that creates the insight
	Synthesize func(ctx context.Context, findings []Finding) *SynthesisResult
	// Confidence boost when this pattern is detected
	ConfidenceBoost float64
}

// SynthesisPrecondition defines a condition for synthesis
type SynthesisPrecondition struct {
	// RequiredTypes are finding types that must be present
	RequiredTypes []string
	// MinCount is minimum number of findings needed
	MinCount int
	// TimeWindow is maximum time span between findings
	TimeWindow time.Duration
	// Custom validation function
	Validate func(findings []Finding) bool
}

// SynthesisResult represents a synthesized insight
type SynthesisResult struct {
	// Pattern identified
	Pattern string
	// High-level explanation
	Insight string
	// Detailed narrative
	Narrative string
	// Action items
	Actions []string
	// Prevention measures
	Prevention []string
	// Business impact
	BusinessImpact string
	// Estimated resolution time
	ResolutionTime time.Duration
	// Confidence in this synthesis
	Confidence float64
}

// SynthesisEngine applies synthesis rules to findings
type SynthesisEngine struct {
	logger *zap.Logger
	rules  []SynthesisRule
}

// NewSynthesisEngine creates a new synthesis engine
func NewSynthesisEngine(logger *zap.Logger) *SynthesisEngine {
	engine := &SynthesisEngine{
		logger: logger,
		rules:  []SynthesisRule{},
	}
	engine.initializeDefaultRules()
	return engine
}

// ApplySynthesis applies synthesis rules to findings
func (e *SynthesisEngine) ApplySynthesis(ctx context.Context, findings []Finding) []SynthesisResult {
	var results []SynthesisResult

	for _, rule := range e.rules {
		if e.checkPreconditions(findings, rule.Preconditions) {
			if result := rule.Synthesize(ctx, findings); result != nil {
				e.logger.Debug("Synthesis rule matched",
					zap.String("rule", rule.Name),
					zap.String("pattern", result.Pattern))
				results = append(results, *result)
			}
		}
	}

	return results
}

// checkPreconditions verifies if all preconditions are met
func (e *SynthesisEngine) checkPreconditions(findings []Finding, preconditions []SynthesisPrecondition) bool {
	for _, precond := range preconditions {
		if !e.checkSinglePrecondition(findings, precond) {
			return false
		}
	}
	return true
}

// checkSinglePrecondition checks a single precondition
func (e *SynthesisEngine) checkSinglePrecondition(findings []Finding, precond SynthesisPrecondition) bool {
	// Check required types
	typeCount := make(map[string]int)
	for _, finding := range findings {
		typeCount[finding.Type]++
	}

	for _, reqType := range precond.RequiredTypes {
		if typeCount[reqType] == 0 {
			return false
		}
	}

	// Check minimum count
	if len(findings) < precond.MinCount {
		return false
	}

	// Check time window
	if precond.TimeWindow > 0 {
		var minTime, maxTime time.Time
		for i, finding := range findings {
			if i == 0 || finding.Timestamp.Before(minTime) {
				minTime = finding.Timestamp
			}
			if i == 0 || finding.Timestamp.After(maxTime) {
				maxTime = finding.Timestamp
			}
		}
		if maxTime.Sub(minTime) > precond.TimeWindow {
			return false
		}
	}

	// Custom validation
	if precond.Validate != nil && !precond.Validate(findings) {
		return false
	}

	return true
}

// initializeDefaultRules sets up built-in synthesis rules
func (e *SynthesisEngine) initializeDefaultRules() {
	e.rules = []SynthesisRule{
		{
			ID:          "death-spiral",
			Name:        "Container Death Spiral",
			Description: "Container repeatedly crashing due to resource constraints",
			Priority:    100,
			Preconditions: []SynthesisPrecondition{
				{
					RequiredTypes: []string{"pod_restart", "oom_kill"},
					MinCount:      2,
					TimeWindow:    10 * time.Minute,
					Validate: func(findings []Finding) bool {
						// Same pod must be involved
						pods := make(map[string]bool)
						for _, f := range findings {
							for _, r := range f.Impact.Resources {
								if strings.Contains(r, "pod/") {
									pods[r] = true
								}
							}
						}
						return len(pods) == 1
					},
				},
			},
			Synthesize: func(ctx context.Context, findings []Finding) *SynthesisResult {
				var podName string
				restartCount := 0
				oomCount := 0

				for _, f := range findings {
					if f.Type == "pod_restart" {
						restartCount++
					}
					if f.Type == "oom_kill" {
						oomCount++
					}
					for _, r := range f.Impact.Resources {
						if strings.Contains(r, "pod/") {
							podName = r
						}
					}
				}

				return &SynthesisResult{
					Pattern: "death-spiral",
					Insight: fmt.Sprintf("Container in %s is in a death spiral with %d restarts and %d OOM kills",
						podName, restartCount, oomCount),
					Narrative: "The container is repeatedly crashing due to insufficient memory. Each restart " +
						"attempts to run with the same memory limit, hits the limit again, and gets killed. " +
						"This creates a loop where the container never achieves a stable state.",
					Actions: []string{
						"Immediately increase memory limits by 50%",
						"Analyze memory usage patterns in application logs",
						"Check for memory leaks in the application",
						"Consider vertical pod autoscaling",
					},
					Prevention: []string{
						"Implement proper memory profiling in CI/CD",
						"Set up memory usage alerts at 80% threshold",
						"Use init containers to validate memory requirements",
						"Enable vertical pod autoscaling for dynamic adjustment",
					},
					BusinessImpact: "Service completely unavailable, affecting all dependent systems",
					ResolutionTime: 15 * time.Minute,
					Confidence:     0.95,
				}
			},
			ConfidenceBoost: 0.15,
		},
		{
			ID:          "config-drift",
			Name:        "Configuration Drift Cascade",
			Description: "Configuration changes causing cascading failures",
			Priority:    95,
			Preconditions: []SynthesisPrecondition{
				{
					RequiredTypes: []string{"config_change", "pod_restart", "connection_error"},
					MinCount:      3,
					TimeWindow:    15 * time.Minute,
				},
			},
			Synthesize: func(ctx context.Context, findings []Finding) *SynthesisResult {
				var configChanges []string
				var affectedServices []string
				serviceMap := make(map[string]bool)

				for _, f := range findings {
					if f.Type == "config_change" {
						configChanges = append(configChanges, f.Message)
					}
					for _, r := range f.Impact.Resources {
						if strings.Contains(r, "service/") {
							if !serviceMap[r] {
								serviceMap[r] = true
								affectedServices = append(affectedServices, r)
							}
						}
					}
				}

				return &SynthesisResult{
					Pattern: "config-drift-cascade",
					Insight: fmt.Sprintf("Configuration changes triggered cascading failures across %d services",
						len(affectedServices)),
					Narrative: "A configuration change has propagated through the system, causing services to " +
						"restart with potentially incompatible settings. This led to connection errors between " +
						"services as they came back online with different configurations.",
					Actions: []string{
						"Verify configuration compatibility across all services",
						"Roll back recent configuration changes if necessary",
						"Restart services in dependency order",
						"Validate inter-service communication",
					},
					Prevention: []string{
						"Implement configuration validation webhooks",
						"Use gradual rollout for configuration changes",
						"Add integration tests for configuration changes",
						"Implement configuration drift detection",
					},
					BusinessImpact: "Partial service degradation with potential data inconsistency",
					ResolutionTime: 30 * time.Minute,
					Confidence:     0.85,
				}
			},
			ConfidenceBoost: 0.10,
		},
		{
			ID:          "noisy-neighbor",
			Name:        "Noisy Neighbor Resource Contention",
			Description: "Multiple pods competing for same node resources",
			Priority:    85,
			Preconditions: []SynthesisPrecondition{
				{
					RequiredTypes: []string{"cpu_throttling", "memory_pressure"},
					MinCount:      2,
					TimeWindow:    5 * time.Minute,
					Validate: func(findings []Finding) bool {
						// Check if on same node
						nodes := make(map[string]bool)
						for _, f := range findings {
							if node, ok := f.Evidence.Attributes["node"]; ok {
								nodes[node] = true
							}
						}
						return len(nodes) == 1
					},
				},
			},
			Synthesize: func(ctx context.Context, findings []Finding) *SynthesisResult {
				affectedPods := make(map[string]bool)
				var nodeName string

				for _, f := range findings {
					for _, r := range f.Impact.Resources {
						if strings.Contains(r, "pod/") {
							affectedPods[r] = true
						}
					}
					if node, ok := f.Evidence.Attributes["node"]; ok {
						nodeName = node
					}
				}

				return &SynthesisResult{
					Pattern: "noisy-neighbor",
					Insight: fmt.Sprintf("%d pods on node %s are competing for resources",
						len(affectedPods), nodeName),
					Narrative: "Multiple pods scheduled on the same node are competing for CPU and memory " +
						"resources. This 'noisy neighbor' effect causes performance degradation for all " +
						"pods on the node as they fight for limited resources.",
					Actions: []string{
						"Identify resource-heavy pods and consider rescheduling",
						"Review resource requests and limits for accuracy",
						"Consider node affinity rules to spread load",
						"Enable pod disruption budgets before rescheduling",
					},
					Prevention: []string{
						"Implement proper resource quotas per namespace",
						"Use pod anti-affinity for resource-intensive workloads",
						"Enable cluster autoscaling for dynamic capacity",
						"Implement quality of service (QoS) classes",
					},
					BusinessImpact: "Performance degradation affecting multiple services",
					ResolutionTime: 20 * time.Minute,
					Confidence:     0.80,
				}
			},
			ConfidenceBoost: 0.08,
		},
		{
			ID:          "security-escalation",
			Name:        "Security Context Escalation",
			Description: "Security policy violations causing service disruptions",
			Priority:    90,
			Preconditions: []SynthesisPrecondition{
				{
					RequiredTypes: []string{"security_violation", "pod_eviction"},
					MinCount:      2,
					TimeWindow:    5 * time.Minute,
				},
			},
			Synthesize: func(ctx context.Context, findings []Finding) *SynthesisResult {
				var violations []string
				for _, f := range findings {
					if f.Type == "security_violation" {
						violations = append(violations, f.Message)
					}
				}

				return &SynthesisResult{
					Pattern: "security-escalation",
					Insight: "Security policy violations are causing pod evictions",
					Narrative: "Pods are being evicted due to security policy violations. This often happens " +
						"when pod security standards are updated or when applications attempt to perform " +
						"privileged operations that violate cluster security policies.",
					Actions: []string{
						"Review pod security policies and violations",
						"Update pod specs to comply with security policies",
						"Audit application requirements for privileged access",
						"Apply proper security contexts to pods",
					},
					Prevention: []string{
						"Implement admission webhooks for security validation",
						"Use OPA/Gatekeeper for policy enforcement",
						"Regular security policy audits",
						"Developer training on security best practices",
					},
					BusinessImpact: "Service unavailability due to security compliance",
					ResolutionTime: 45 * time.Minute,
					Confidence:     0.90,
				}
			},
			ConfidenceBoost: 0.12,
		},
		{
			ID:          "storage-starvation",
			Name:        "Storage Starvation Cascade",
			Description: "Storage issues causing application failures",
			Priority:    88,
			Preconditions: []SynthesisPrecondition{
				{
					RequiredTypes: []string{"storage_full", "write_error", "pod_crash"},
					MinCount:      3,
					TimeWindow:    10 * time.Minute,
				},
			},
			Synthesize: func(ctx context.Context, findings []Finding) *SynthesisResult {
				var affectedPVCs []string
				pvcMap := make(map[string]bool)

				for _, f := range findings {
					for _, r := range f.Impact.Resources {
						if strings.Contains(r, "pvc/") {
							if !pvcMap[r] {
								pvcMap[r] = true
								affectedPVCs = append(affectedPVCs, r)
							}
						}
					}
				}

				return &SynthesisResult{
					Pattern: "storage-starvation",
					Insight: fmt.Sprintf("Storage exhaustion affecting %d persistent volume claims", len(affectedPVCs)),
					Narrative: "Applications are crashing due to inability to write data to persistent storage. " +
						"This creates a cascade where services cannot persist state, leading to data loss " +
						"and service failures.",
					Actions: []string{
						"Immediately expand PVC capacity if possible",
						"Identify and clean up unnecessary data",
						"Implement log rotation if logs are filling storage",
						"Consider moving to object storage for large datasets",
					},
					Prevention: []string{
						"Implement storage monitoring and alerting at 80% capacity",
						"Use dynamic volume provisioning with expansion enabled",
						"Implement data lifecycle policies",
						"Regular storage capacity planning reviews",
					},
					BusinessImpact: "Data loss risk and service unavailability",
					ResolutionTime: 60 * time.Minute,
					Confidence:     0.92,
				}
			},
			ConfidenceBoost: 0.10,
		},
	}
}

// AddRule adds a custom synthesis rule
func (e *SynthesisEngine) AddRule(rule SynthesisRule) {
	e.rules = append(e.rules, rule)
	// Re-sort by priority
	for i := 0; i < len(e.rules)-1; i++ {
		for j := i + 1; j < len(e.rules); j++ {
			if e.rules[j].Priority > e.rules[i].Priority {
				e.rules[i], e.rules[j] = e.rules[j], e.rules[i]
			}
		}
	}
}

// GetRule returns a rule by ID
func (e *SynthesisEngine) GetRule(id string) *SynthesisRule {
	for _, rule := range e.rules {
		if rule.ID == id {
			return &rule
		}
	}
	return nil
}

// RemoveRule removes a rule by ID
func (e *SynthesisEngine) RemoveRule(id string) bool {
	for i, rule := range e.rules {
		if rule.ID == id {
			e.rules = append(e.rules[:i], e.rules[i+1:]...)
			return true
		}
	}
	return false
}
