package rules

import (
	"fmt"

	"github.com/yairfalse/tapio/pkg/correlation"
)

// RegisterAll registers all built-in correlation rules with the engine
func RegisterAll(engine correlation.Engine) error {
	rules := []*correlation.Rule{
		// Memory-related rules
		MemoryPressureCascade(),
		MemoryLeakDetection(),
		ContainerOOMPrediction(),

		// CPU-related rules
		CPUThrottleDetection(),
		CPUContentionDetection(),
		HighCPUUtilizationPattern(),
	}

	registeredCount := 0
	for _, rule := range rules {
		if err := engine.RegisterRule(rule); err != nil {
			return fmt.Errorf("failed to register rule %s: %w", rule.ID, err)
		}
		registeredCount++
	}

	fmt.Printf("✅ Successfully registered %d correlation rules\n", registeredCount)
	return nil
}

// GetRuleByID returns a rule by its ID
func GetRuleByID(id string) *correlation.Rule {
	rules := map[string]func() *correlation.Rule{
		"memory-pressure-cascade":      MemoryPressureCascade,
		"memory-leak-detection":        MemoryLeakDetection,
		"container-oom-prediction":     ContainerOOMPrediction,
		"cpu-throttle-detection":       CPUThrottleDetection,
		"cpu-contention-detection":     CPUContentionDetection,
		"high-cpu-utilization-pattern": HighCPUUtilizationPattern,
	}

	if ruleFn, exists := rules[id]; exists {
		return ruleFn()
	}

	return nil
}

// GetRulesByCategory returns all rules for a specific category
func GetRulesByCategory(category correlation.Category) []*correlation.Rule {
	allRules := []*correlation.Rule{
		MemoryPressureCascade(),
		MemoryLeakDetection(),
		ContainerOOMPrediction(),
		CPUThrottleDetection(),
		CPUContentionDetection(),
		HighCPUUtilizationPattern(),
	}

	var filtered []*correlation.Rule
	for _, rule := range allRules {
		if rule.Category == category {
			filtered = append(filtered, rule)
		}
	}

	return filtered
}

// GetRulesByTag returns all rules that have a specific tag
func GetRulesByTag(tag string) []*correlation.Rule {
	allRules := []*correlation.Rule{
		MemoryPressureCascade(),
		MemoryLeakDetection(),
		ContainerOOMPrediction(),
		CPUThrottleDetection(),
		CPUContentionDetection(),
		HighCPUUtilizationPattern(),
	}

	var filtered []*correlation.Rule
	for _, rule := range allRules {
		for _, ruleTag := range rule.Tags {
			if ruleTag == tag {
				filtered = append(filtered, rule)
				break
			}
		}
	}

	return filtered
}

// ListAllRules returns all available rules
func ListAllRules() []*correlation.Rule {
	return []*correlation.Rule{
		MemoryPressureCascade(),
		MemoryLeakDetection(),
		ContainerOOMPrediction(),
		CPUThrottleDetection(),
		CPUContentionDetection(),
		HighCPUUtilizationPattern(),
	}
}

// RuleSummary contains summary information about available rules
type RuleSummary struct {
	ID          string                    `json:"id"`
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Category    correlation.Category      `json:"category"`
	Tags        []string                  `json:"tags"`
	Sources     []correlation.EventSource `json:"sources"`
}

// GetRuleSummaries returns summary information for all rules
func GetRuleSummaries() []RuleSummary {
	rules := ListAllRules()
	summaries := make([]RuleSummary, 0, len(rules))

	for _, rule := range rules {
		sources := append(rule.RequiredSources, rule.OptionalSources...)

		summaries = append(summaries, RuleSummary{
			ID:          rule.ID,
			Name:        rule.Name,
			Description: rule.Description,
			Category:    rule.Category,
			Tags:        rule.Tags,
			Sources:     sources,
		})
	}

	return summaries
}

// ValidateRule performs basic validation on a rule
func ValidateRule(rule *correlation.Rule) error {
	if rule == nil {
		return fmt.Errorf("rule cannot be nil")
	}

	if rule.ID == "" {
		return fmt.Errorf("rule ID is required")
	}

	if rule.Name == "" {
		return fmt.Errorf("rule name is required")
	}

	if rule.Evaluate == nil {
		return fmt.Errorf("rule must have an Evaluate function")
	}

	if rule.MinConfidence < 0 || rule.MinConfidence > 1 {
		return fmt.Errorf("rule MinConfidence must be between 0 and 1")
	}

	if len(rule.RequiredSources) == 0 && len(rule.OptionalSources) == 0 {
		return fmt.Errorf("rule must specify at least one required or optional source")
	}

	return nil
}
