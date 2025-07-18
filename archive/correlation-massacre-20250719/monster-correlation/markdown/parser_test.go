package markdown

import (
	"strings"
	"testing"
	"time"
)

func TestCorrelationMarkdownParser(t *testing.T) {
	tests := []struct {
		name     string
		markdown string
		expected int // expected number of correlations
	}{
		{
			name: "simple threshold pattern",
			markdown: `## Memory Usage Pattern

When memory > 80% for 5 minutes,
then alert high memory usage.

Severity: high`,
			expected: 1,
		},
		{
			name: "multiple conditions with AND",
			markdown: `## Complex Pattern

When CPU > 90% and memory > 80%,
then predict system overload.

Root cause: Resource constraints`,
			expected: 1,
		},
		{
			name: "multiple patterns",
			markdown: `## Pattern One

When disk > 90%, then alert disk full.

## Pattern Two  

If network latency > 100ms for 30 seconds,
then check network connectivity.`,
			expected: 2,
		},
		{
			name: "pattern with yaml metadata",
			markdown: `## Database Failure

When database errors > 10 per second,
then cascade failure likely.

` + "```yaml\n" + `severity: critical
confidence: 95
category: database
` + "```",
			expected: 1,
		},
	}

	parser := NewCorrelationMarkdownParser()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			correlations, err := parser.ParseCorrelationMarkdown(tt.markdown)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(correlations) != tt.expected {
				t.Errorf("expected %d correlations, got %d", tt.expected, len(correlations))
			}

			// Validate first correlation if exists
			if len(correlations) > 0 {
				corr := correlations[0]
				if corr.Name == "" {
					t.Error("correlation name should not be empty")
				}
				if len(corr.Conditions) == 0 {
					t.Error("correlation should have at least one condition")
				}
				if len(corr.Actions) == 0 {
					t.Error("correlation should have at least one action")
				}
			}
		})
	}
}

func TestConditionParsing(t *testing.T) {
	parser := NewCorrelationMarkdownParser()

	tests := []struct {
		name             string
		markdown         string
		expectedType     string
		expectedValue    string
		expectedUnit     string
		expectedDuration time.Duration
	}{
		{
			name:          "percentage threshold",
			markdown:      "## Test\nWhen memory > 80%",
			expectedType:  "threshold",
			expectedValue: "80",
			expectedUnit:  "%",
		},
		{
			name:             "threshold with duration",
			markdown:         "## Test\nWhen CPU > 90% for 5 minutes",
			expectedType:     "threshold",
			expectedValue:    "90",
			expectedUnit:     "%",
			expectedDuration: 5 * time.Minute,
		},
		{
			name:          "milliseconds threshold",
			markdown:      "## Test\nIf latency > 500ms",
			expectedType:  "threshold",
			expectedValue: "500",
			expectedUnit:  "ms",
		},
		{
			name:         "text condition",
			markdown:     "## Test\nWhen service is unhealthy",
			expectedType: "text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			correlations, err := parser.ParseCorrelationMarkdown(tt.markdown)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(correlations) != 1 {
				t.Fatalf("expected 1 correlation, got %d", len(correlations))
			}

			if len(correlations[0].Conditions) == 0 {
				t.Fatal("expected at least one condition")
			}

			cond := correlations[0].Conditions[0]
			if cond.Type != tt.expectedType {
				t.Errorf("expected type %s, got %s", tt.expectedType, cond.Type)
			}

			if tt.expectedType == "threshold" {
				if cond.Value != tt.expectedValue {
					t.Errorf("expected value %s, got %s", tt.expectedValue, cond.Value)
				}
				if cond.Unit != tt.expectedUnit {
					t.Errorf("expected unit %s, got %s", tt.expectedUnit, cond.Unit)
				}
				if cond.Duration != tt.expectedDuration {
					t.Errorf("expected duration %v, got %v", tt.expectedDuration, cond.Duration)
				}
			}
		})
	}
}

func TestActionParsing(t *testing.T) {
	parser := NewCorrelationMarkdownParser()

	tests := []struct {
		name         string
		markdown     string
		expectedType string
		contains     string
	}{
		{
			name:         "root cause",
			markdown:     "## Test\nWhen x > 1, then y happens.\nRoot cause: Database overload",
			expectedType: "root_cause",
			contains:     "Database overload",
		},
		{
			name:         "prediction",
			markdown:     "## Test\nWhen x > 1\nPredict: System will crash in 5 minutes",
			expectedType: "prediction",
			contains:     "System will crash",
		},
		{
			name:         "recommendation",
			markdown:     "## Test\nWhen x > 1\nRecommend: Scale up the service",
			expectedType: "recommendation",
			contains:     "Scale up",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			correlations, err := parser.ParseCorrelationMarkdown(tt.markdown)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(correlations) != 1 {
				t.Fatalf("expected 1 correlation, got %d", len(correlations))
			}

			// Find action of expected type
			found := false
			for _, action := range correlations[0].Actions {
				if action.Type == tt.expectedType {
					found = true
					if !strings.Contains(action.Description, tt.contains) {
						t.Errorf("expected action to contain '%s', got '%s'",
							tt.contains, action.Description)
					}
					break
				}
			}

			if !found {
				t.Errorf("expected to find action of type %s", tt.expectedType)
			}
		})
	}
}

func TestMetadataParsing(t *testing.T) {
	parser := NewCorrelationMarkdownParser()

	markdown := `## Test Pattern

When CPU > 90%,
then alert.

Severity: critical
Confidence: 85%
Category: performance

` + "```yaml\n" + `custom_field: custom_value
another_field: 123
` + "```"

	correlations, err := parser.ParseCorrelationMarkdown(markdown)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(correlations) != 1 {
		t.Fatalf("expected 1 correlation, got %d", len(correlations))
	}

	metadata := correlations[0].Metadata

	// Check inline metadata
	if metadata["severity"] != "critical" {
		t.Errorf("expected severity 'critical', got '%v'", metadata["severity"])
	}
	if metadata["confidence"] != "85" {
		t.Errorf("expected confidence '85', got '%v'", metadata["confidence"])
	}
	if metadata["category"] != "performance" {
		t.Errorf("expected category 'performance', got '%v'", metadata["category"])
	}

	// Check YAML metadata
	if metadata["custom_field"] != "custom_value" {
		t.Errorf("expected custom_field 'custom_value', got '%v'", metadata["custom_field"])
	}
}
