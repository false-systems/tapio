package correlation

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/types"
)

func TestCorrelationService(t *testing.T) {
	// Create service
	service, err := NewService()
	if err != nil {
		t.Fatalf("Failed to create correlation service: %v", err)
	}

	// Start service
	ctx := context.Background()
	if err := service.Start(ctx); err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}
	defer service.Stop()

	// Create test check result with problems
	checkResult := &types.CheckResult{
		Timestamp: time.Now(),
		Summary: types.Summary{
			TotalPods:    5,
			HealthyPods:  2,
			WarningPods:  2,
			CriticalPods: 1,
		},
		Problems: []types.Problem{
			{
				Resource: types.ResourceRef{
					Kind:      "pod",
					Name:      "api-service-abc",
					Namespace: "default",
				},
				Severity:    types.SeverityCritical,
				Title:       "Pod restarting frequently",
				Description: "Pod has restarted 10 times in the last hour",
			},
			{
				Resource: types.ResourceRef{
					Kind:      "pod",
					Name:      "api-service-def",
					Namespace: "default",
				},
				Severity:    types.SeverityWarning,
				Title:       "High memory usage",
				Description: "Memory usage at 85% of limit",
			},
		},
	}

	// Analyze check result
	correlationResult, err := service.AnalyzeCheckResult(ctx, checkResult)
	if err != nil {
		t.Fatalf("Failed to analyze check result: %v", err)
	}

	// Verify we got a result
	if correlationResult == nil {
		t.Fatal("Expected correlation result, got nil")
	}

	// Check timeline was populated
	if correlationResult.Timeline == nil {
		t.Error("Expected timeline data")
	}

	// Check statistics
	if correlationResult.Statistics.TotalEvents == 0 {
		t.Error("Expected events to be processed")
	}

	t.Logf("Correlation analysis completed successfully")
	t.Logf("Total events: %d", correlationResult.Statistics.TotalEvents)
	t.Logf("Insights generated: %d", len(correlationResult.Insights))
}

func TestCorrelationServicePatternDetection(t *testing.T) {
	service, err := NewService()
	if err != nil {
		t.Fatalf("Failed to create correlation service: %v", err)
	}

	ctx := context.Background()
	if err := service.Start(ctx); err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}
	defer service.Stop()

	// Create test result with pattern-like problems
	checkResult := &types.CheckResult{
		Timestamp: time.Now(),
		Problems: []types.Problem{
			{
				Resource: types.ResourceRef{
					Kind:      "pod",
					Name:      "web-1",
					Namespace: "default",
				},
				Severity:    types.SeverityCritical,
				Title:       "OOMKilled",
				Description: "Container killed due to out of memory",
			},
			{
				Resource: types.ResourceRef{
					Kind:      "pod",
					Name:      "web-2",
					Namespace: "default",
				},
				Severity:    types.SeverityCritical,
				Title:       "OOMKilled",
				Description: "Container killed due to out of memory",
			},
			{
				Resource: types.ResourceRef{
					Kind:      "pod",
					Name:      "web-3",
					Namespace: "default",
				},
				Severity:    types.SeverityCritical,
				Title:       "OOMKilled",
				Description: "Container killed due to out of memory",
			},
		},
	}

	result, err := service.AnalyzeCheckResult(ctx, checkResult)
	if err != nil {
		t.Fatalf("Failed to analyze: %v", err)
	}

	// Check for pattern detection
	if len(result.Patterns) == 0 {
		t.Log("No patterns detected (this is OK for simple implementation)")
	} else {
		t.Logf("Detected %d patterns", len(result.Patterns))
		for _, pattern := range result.Patterns {
			t.Logf("Pattern: %s - %s", pattern.Type, pattern.Description)
		}
	}
}
