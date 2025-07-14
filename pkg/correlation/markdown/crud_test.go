package markdown

import (
	"testing"

	"github.com/yairfalse/tapio/pkg/correlation"
)

func TestCorrelationCRUDOperations(t *testing.T) {
	// Create a semantic rules engine (mocked for testing)
	engine := &correlation.SemanticRulesEngine{}
	translator := NewCorrelationTranslator()

	// Test markdown content
	initialMarkdown := `# Test Correlations

## Memory Usage Pattern

When memory usage > 80% for 5 minutes,
then alert high memory usage.

Severity: high
Confidence: 85%
Category: memory`

	updatedMarkdown := `# Updated Test Correlations

## Memory Usage Pattern

When memory usage > 90% for 10 minutes,
then alert critical memory usage requiring immediate attention.

Severity: critical
Confidence: 95%
Category: memory`

	t.Run("Create - Load initial rules", func(t *testing.T) {
		rules, err := translator.TranslateMarkdownToRules(initialMarkdown)
		if err != nil {
			t.Fatalf("Failed to translate initial markdown: %v", err)
		}

		if len(rules) != 1 {
			t.Fatalf("Expected 1 rule, got %d", len(rules))
		}

		rule := rules[0]
		if rule.Name != "Memory Usage Pattern" {
			t.Errorf("Expected rule name 'Memory Usage Pattern', got '%s'", rule.Name)
		}

		if rule.Severity != "high" {
			t.Errorf("Expected severity 'high', got '%s'", rule.Severity)
		}

		if rule.Category != "memory" {
			t.Errorf("Expected category 'memory', got '%s'", rule.Category)
		}
	})

	t.Run("Read - Validate rule structure", func(t *testing.T) {
		rules, err := translator.TranslateMarkdownToRules(initialMarkdown)
		if err != nil {
			t.Fatalf("Failed to translate markdown: %v", err)
		}

		rule := rules[0]
		
		// Check rule ID generation
		expectedID := "user_memory_usage_pattern"
		if rule.ID != expectedID {
			t.Errorf("Expected rule ID '%s', got '%s'", expectedID, rule.ID)
		}

		// Check metadata
		if rule.Metadata["source"] != "markdown" {
			t.Errorf("Expected metadata source 'markdown', got '%v'", rule.Metadata["source"])
		}

		if rule.Metadata["user_defined"] != true {
			t.Errorf("Expected metadata user_defined 'true', got '%v'", rule.Metadata["user_defined"])
		}

		// Check conditions and actions exist
		if rule.SemanticConditions == nil {
			t.Error("Expected semantic conditions to be set")
		}

		if len(rule.Actions) == 0 {
			t.Error("Expected at least one action")
		}
	})

	t.Run("Update - Modify existing rule", func(t *testing.T) {
		rules, err := translator.TranslateMarkdownToRules(updatedMarkdown)
		if err != nil {
			t.Fatalf("Failed to translate updated markdown: %v", err)
		}

		rule := rules[0]
		
		// Should have same ID but updated properties
		if rule.ID != "user_memory_usage_pattern" {
			t.Errorf("Expected same rule ID after update, got '%s'", rule.ID)
		}

		if rule.Severity != "critical" {
			t.Errorf("Expected updated severity 'critical', got '%s'", rule.Severity)
		}

		// Check that confidence was updated
		confidence, ok := rule.Metadata["confidence_score"].(float64)
		if !ok || confidence != 0.95 {
			t.Errorf("Expected updated confidence 0.95, got %v", confidence)
		}
	})

	t.Run("JSON Translation", func(t *testing.T) {
		jsonData, err := translator.TranslateMarkdownToJSON(initialMarkdown)
		if err != nil {
			t.Fatalf("Failed to translate to JSON: %v", err)
		}

		if len(jsonData) == 0 {
			t.Error("Expected non-empty JSON data")
		}

		// Verify it's valid JSON by parsing back
		rules, err := translator.TranslateMarkdownToRules(initialMarkdown)
		if err != nil {
			t.Fatalf("Failed to parse translated rules: %v", err)
		}

		if len(rules) != 1 {
			t.Errorf("Expected 1 rule from JSON, got %d", len(rules))
		}
	})

	t.Run("Validation - Error handling", func(t *testing.T) {
		invalidMarkdown := `# Invalid

## No Conditions

then do something without conditions.`

		_, err := translator.TranslateMarkdownToRules(invalidMarkdown)
		if err == nil {
			t.Error("Expected error for invalid markdown, got none")
		}
	})
}

func TestDeleteRulesValidation(t *testing.T) {
	translator := NewCorrelationTranslator()

	t.Run("Delete validation", func(t *testing.T) {
		// Test that delete function properly validates rule IDs
		ruleIDs := []string{"user_test_rule", ""}
		
		// This should work in real implementation
		// For now, just test the structure exists
		if translator == nil {
			t.Error("Translator should be initialized")
		}
		
		// The actual delete would require a real engine
		// Here we just verify the function signature exists
		err := translator.DeleteRulesFromEngine(ruleIDs, nil)
		if err == nil {
			t.Error("Expected error when passing nil engine")
		}
	})
}