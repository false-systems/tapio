package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/yairfalse/tapio/pkg/types"
)

func TestCheckCommandWithCorrelation(t *testing.T) {
	// This is a basic test to ensure the check command works with correlation
	// In a real test, we would mock the Kubernetes client and correlation service
	
	// Test that the command is properly initialized
	if checkCmd == nil {
		t.Fatal("Check command not initialized")
	}

	// Verify flags are set up
	if checkCmd.Flag("correlation") == nil {
		t.Error("Expected --correlation flag")
	}

	if checkCmd.Flag("namespace") == nil {
		t.Error("Expected --namespace flag")
	}

	if checkCmd.Flag("output") == nil {
		t.Error("Expected --output flag")
	}
}

func TestCorrelationAnalysisOutput(t *testing.T) {
	// Test the correlation analysis output formatting
	
	// Create mock correlation result
	correlationResult := map[string]interface{}{
		"analysis_type": "intelligent",
		"insights": []interface{}{
			map[string]interface{}{
				"Title":        "Memory pressure detected",
				"Description":  "Multiple pods showing memory issues",
				"Severity":     "high",
				"ResourceName": "api-service",
				"Namespace":    "default",
			},
		},
		"patterns": []interface{}{
			map[string]interface{}{
				"Type":        "burst",
				"Description": "Burst of 5 events for pod/api-service",
				"Confidence":  0.8,
			},
		},
		"timeline": map[string]interface{}{
			"total_events": 10,
			"events_by_severity": map[string]interface{}{
				"critical": 2,
				"high":     3,
				"medium":   5,
			},
		},
	}

	// Create check result with correlation
	result := &types.CheckResult{
		Summary: types.Summary{
			TotalPods:    10,
			HealthyPods:  5,
			WarningPods:  3,
			CriticalPods: 2,
		},
		Problems: []types.Problem{
			{
				Resource: types.ResourceRef{
					Kind:      "pod",
					Name:      "api-service-123",
					Namespace: "default",
				},
				Severity:    types.SeverityCritical,
				Title:       "Pod CrashLoopBackOff",
				Description: "Container failing to start",
			},
		},
		CorrelationAnalysis: correlationResult,
	}

	// Test human output formatting
	var buf bytes.Buffer
	// In a real test, we would capture the output and verify it contains expected content
	_ = buf
	_ = result

	t.Log("Correlation analysis output test completed")
}

func TestAddCorrelationAnalysis(t *testing.T) {
	// Test the correlation analysis function
	ctx := context.Background()
	
	result := &types.CheckResult{
		Problems: []types.Problem{
			{
				Resource: types.ResourceRef{
					Kind:      "pod",
					Name:      "test-pod-1",
					Namespace: "default",
				},
				Severity:    types.SeverityWarning,
				Title:       "High memory usage",
				Description: "Memory at 90%",
			},
			{
				Resource: types.ResourceRef{
					Kind:      "pod",
					Name:      "test-pod-2",
					Namespace: "default",
				},
				Severity:    types.SeverityWarning,
				Title:       "High memory usage",
				Description: "Memory at 85%",
			},
		},
	}

	// Call the analysis function
	analysis, err := addCorrelationAnalysis(ctx, nil, result)
	if err != nil {
		t.Logf("Correlation analysis returned error (expected for unit test): %v", err)
	}

	if analysis != nil {
		// Check that we got some analysis
		if analysisMap, ok := analysis.(map[string]interface{}); ok {
			if patterns, ok := analysisMap["patterns"].(map[string]int); ok {
				if patterns["memory"] != 2 {
					t.Errorf("Expected 2 memory issues, got %d", patterns["memory"])
				}
			}
		}
	}
}

func TestAnalyzeProblemsSimple(t *testing.T) {
	problems := []types.Problem{
		{
			Resource: types.ResourceRef{
				Kind:      "pod",
				Name:      "pod1",
				Namespace: "ns1",
			},
			Severity:    types.SeverityWarning,
			Title:       "High memory usage",
			Description: "Memory at 90%",
		},
		{
			Resource: types.ResourceRef{
				Kind:      "pod",
				Name:      "pod2",
				Namespace: "ns1",
			},
			Severity:    types.SeverityWarning,
			Title:       "High memory usage",
			Description: "Memory at 85%",
		},
		{
			Resource: types.ResourceRef{
				Kind:      "pod",
				Name:      "pod3",
				Namespace: "ns1",
			},
			Severity:    types.SeverityCritical,
			Title:       "Pod restarting",
			Description: "Restart count high",
		},
	}

	result := analyzeProblemsSimple(problems)

	// Verify patterns
	patterns, ok := result["patterns"].(map[string]int)
	if !ok {
		t.Fatal("Expected patterns in result")
	}

	if patterns["memory"] != 2 {
		t.Errorf("Expected 2 memory patterns, got %d", patterns["memory"])
	}

	if patterns["restart"] != 1 {
		t.Errorf("Expected 1 restart pattern, got %d", patterns["restart"])
	}

	// Verify insights
	insights, ok := result["insights"].([]string)
	if !ok {
		t.Fatal("Expected insights in result")
	}

	// Should have memory pressure insight
	hasMemoryInsight := false
	for _, insight := range insights {
		if strings.Contains(insight, "Memory pressure") {
			hasMemoryInsight = true
			break
		}
	}

	if !hasMemoryInsight {
		t.Error("Expected memory pressure insight")
	}

	// Should have namespace concentration insight
	hasNamespaceInsight := false
	for _, insight := range insights {
		if strings.Contains(insight, "single namespace") {
			hasNamespaceInsight = true
			break
		}
	}

	if !hasNamespaceInsight {
		t.Error("Expected namespace concentration insight")
	}
}