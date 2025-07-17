package rules
import (
	"fmt"
	"github.com/falseyair/tapio/pkg/intelligence/correlation"
)
// RegisterAll registers all built-in correlation rules with the engine
func RegisterAll(engine correlation.Engine) error {
	// Create rule instances
	rules := []correlation.Rule{
		// Memory-related rules
		NewMemoryLeakRule(DefaultMemoryLeakConfig()),
		// CPU-related rules
		NewCPUThrottlingRule(DefaultCPUThrottlingConfig()),
		// Certificate-related rules
		NewCertificateCascadeRule(DefaultCertificateCascadeConfig()),
		// Admission-related rules
		NewAdmissionLockdownRule(DefaultAdmissionLockdownConfig()),
		// Crash loop detection
		NewCrashLoopRule(DefaultCrashLoopConfig()),
	}
	registeredCount := 0
	for _, rule := range rules {
		if err := engine.RegisterRule(rule); err != nil {
			return fmt.Errorf("failed to register rule %s: %w", rule.GetID(), err)
		}
		registeredCount++
	}
	fmt.Printf("✅ Successfully registered %d correlation rules\n", registeredCount)
	return nil
}
// GetRuleByID returns a rule by its ID
func GetRuleByID(id string) correlation.Rule {
	rules := map[string]func() correlation.Rule{
		"memory_leak":                 func() correlation.Rule { return NewMemoryLeakRule(DefaultMemoryLeakConfig()) },
		"cpu_throttling":              func() correlation.Rule { return NewCPUThrottlingRule(DefaultCPUThrottlingConfig()) },
		"certificate_chain_failure":   func() correlation.Rule { return NewCertificateCascadeRule(DefaultCertificateCascadeConfig()) },
		"admission_controller_lockdown": func() correlation.Rule { return NewAdmissionLockdownRule(DefaultAdmissionLockdownConfig()) },
		"crash_loop_detection":        func() correlation.Rule { return NewCrashLoopRule(DefaultCrashLoopConfig()) },
	}
	if ruleFn, exists := rules[id]; exists {
		return ruleFn()
	}
	return nil
}
// GetRulesByCategory returns all rules for a specific category
func GetRulesByCategory(category correlation.Category) []correlation.Rule {
	allRules := []correlation.Rule{
		NewMemoryLeakRule(DefaultMemoryLeakConfig()),
		NewCPUThrottlingRule(DefaultCPUThrottlingConfig()),
		NewCertificateCascadeRule(DefaultCertificateCascadeConfig()),
		NewAdmissionLockdownRule(DefaultAdmissionLockdownConfig()),
		NewCrashLoopRule(DefaultCrashLoopConfig()),
	}
	var filtered []correlation.Rule
	for _, rule := range allRules {
		// Rules don't have a direct Category field anymore
		// We'll need to check via metadata or tags
		metadata := rule.GetMetadata()
		for _, tag := range metadata.Tags {
			if categoryMatchesTag(category, tag) {
				filtered = append(filtered, rule)
				break
			}
		}
	}
	return filtered
}
// GetRulesByTag returns all rules that have a specific tag
func GetRulesByTag(tag string) []correlation.Rule {
	allRules := []correlation.Rule{
		NewMemoryLeakRule(DefaultMemoryLeakConfig()),
		NewCPUThrottlingRule(DefaultCPUThrottlingConfig()),
		NewCertificateCascadeRule(DefaultCertificateCascadeConfig()),
		NewAdmissionLockdownRule(DefaultAdmissionLockdownConfig()),
		NewCrashLoopRule(DefaultCrashLoopConfig()),
	}
	var filtered []correlation.Rule
	for _, rule := range allRules {
		metadata := rule.GetMetadata()
		for _, ruleTag := range metadata.Tags {
			if ruleTag == tag {
				filtered = append(filtered, rule)
				break
			}
		}
	}
	return filtered
}
// ListAllRules returns all available rules
func ListAllRules() []correlation.Rule {
	return []correlation.Rule{
		NewMemoryLeakRule(DefaultMemoryLeakConfig()),
		NewCPUThrottlingRule(DefaultCPUThrottlingConfig()),
		NewCertificateCascadeRule(DefaultCertificateCascadeConfig()),
		NewAdmissionLockdownRule(DefaultAdmissionLockdownConfig()),
		NewCrashLoopRule(DefaultCrashLoopConfig()),
	}
}
// RuleSummary contains summary information about available rules
type RuleSummary struct {
	ID          string                    `json:"id"`
	Name        string                    `json:"name"`
	Description string                    `json:"description"`
	Category    correlation.Category      `json:"category"`
	Tags        []string                  `json:"tags"`
	Sources     []correlation.SourceType `json:"sources"`
}
// GetRuleSummaries returns summary information for all rules
func GetRuleSummaries() []RuleSummary {
	rules := ListAllRules()
	summaries := make([]RuleSummary, 0, len(rules))
	for _, rule := range rules {
		metadata := rule.GetMetadata()
		// Extract source types from requirements
		var sources []correlation.SourceType
		for _, req := range metadata.Requirements {
			sources = append(sources, req.SourceType)
		}
		summaries = append(summaries, RuleSummary{
			ID:          metadata.ID,
			Name:        metadata.Name,
			Description: metadata.Description,
			Category:    correlation.Category("general"), // Default category
			Tags:        metadata.Tags,
			Sources:     sources,
		})
	}
	return summaries
}
// ValidateRule performs basic validation on a rule
func ValidateRule(rule correlation.Rule) error {
	if rule == nil {
		return fmt.Errorf("rule cannot be nil")
	}
	metadata := rule.GetMetadata()
	if metadata.ID == "" {
		return fmt.Errorf("rule ID is required")
	}
	if metadata.Name == "" {
		return fmt.Errorf("rule name is required")
	}
	// Check if rule can be validated
	if validator, ok := rule.(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			return err
		}
	}
	if len(metadata.Requirements) == 0 {
		return fmt.Errorf("rule must specify at least one requirement")
	}
	return nil
}
// categoryMatchesTag checks if a category matches a tag
func categoryMatchesTag(category correlation.Category, tag string) bool {
	// Simple mapping for now
	categoryTags := map[correlation.Category][]string{
		correlation.Category("memory"):      {"memory", "oom", "leak"},
		correlation.Category("cpu"):         {"cpu", "throttling", "performance"},
		correlation.Category("certificate"): {"certificate", "tls", "security"},
		correlation.Category("admission"):   {"admission", "policy", "security"},
		correlation.Category("stability"):   {"stability", "crash", "restart"},
	}
	if tags, ok := categoryTags[category]; ok {
		for _, catTag := range tags {
			if catTag == tag {
				return true
			}
		}
	}
	return false
}
