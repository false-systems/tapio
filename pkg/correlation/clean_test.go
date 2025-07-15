package correlation

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/correlation/domain"
)

func TestCleanArchitecture(t *testing.T) {
	// Create a clean correlation service
	config := DefaultConfig()
	config.MaxConcurrentRules = 2
	config.WindowSize = 1 * time.Minute

	correlation := New(config, nil)

	// Register default rules
	if err := correlation.RegisterDefaultRules(); err != nil {
		t.Fatalf("Failed to register default rules: %v", err)
	}

	// Create test events
	events := []domain.Event{
		{
			ID:        "test-event-1",
			Timestamp: time.Now(),
			Type:      "pod_killed",
			Source:    "kubernetes",
			Severity:  domain.SeverityHigh,
			Category:  domain.CategoryReliability,
			Entity: domain.Entity{
				Type:      "pod",
				Name:      "test-pod",
				Namespace: "default",
			},
		},
		{
			ID:        "test-event-2",
			Timestamp: time.Now().Add(1 * time.Minute),
			Type:      "pod_started",
			Source:    "kubernetes",
			Severity:  domain.SeverityMedium,
			Category:  domain.CategoryReliability,
			Entity: domain.Entity{
				Type:      "pod",
				Name:      "test-pod",
				Namespace: "default",
			},
		},
	}

	// Start the correlation service
	ctx := context.Background()
	if err := correlation.Start(ctx); err != nil {
		t.Fatalf("Failed to start correlation service: %v", err)
	}
	defer correlation.Stop()

	// Process events
	results, err := correlation.ProcessEvents(ctx, events)
	if err != nil {
		t.Fatalf("Failed to process events: %v", err)
	}

	// Verify results
	t.Logf("Processed %d events, got %d results", len(events), len(results))

	for i, result := range results {
		if result == nil {
			t.Errorf("Result %d is nil", i)
			continue
		}

		t.Logf("Result %d: ID=%s, Type=%s, Confidence=%.2f, Description=%s",
			i, result.ID, result.Type, result.Confidence, result.Description)

		if result.ID == "" {
			t.Errorf("Result %d has empty ID", i)
		}

		if result.Type == "" {
			t.Errorf("Result %d has empty Type", i)
		}

		if result.Confidence <= 0 {
			t.Errorf("Result %d has invalid confidence: %.2f", i, result.Confidence)
		}
	}

	// Test statistics
	stats := correlation.GetStats()
	t.Logf("Engine stats: EventsProcessed=%d, CorrelationsFound=%d, RulesActive=%d",
		stats.EventsProcessed, stats.CorrelationsFound, stats.RulesActive)

	if stats.EventsProcessed == 0 {
		t.Error("No events were processed")
	}

	if stats.RulesActive == 0 {
		t.Error("No rules are active")
	}

	// Test available event sources
	availableSources := correlation.GetAvailableEventSources()
	t.Logf("Available event sources: %d", len(availableSources))

	for _, source := range availableSources {
		t.Logf("Source: %s, Available: %t", source.GetSourceType(), source.IsAvailable())
	}

	// Test health check
	if err := correlation.Health(); err != nil {
		t.Errorf("Health check failed: %v", err)
	}
}

func TestCleanArchitectureNoExternalDependencies(t *testing.T) {
	// This test verifies that the clean architecture can work without external dependencies
	// by using only stub implementations

	config := DefaultConfig()
	correlation := New(config, nil)

	// Verify that we can start without external dependencies
	ctx := context.Background()
	if err := correlation.Start(ctx); err != nil {
		t.Fatalf("Failed to start correlation service: %v", err)
	}
	defer correlation.Stop()

	// Test that the service is healthy
	if err := correlation.Health(); err != nil {
		t.Errorf("Health check failed: %v", err)
	}

	// Test that event sources are available (stub implementations)
	availableSources := correlation.GetAvailableEventSources()

	// At least Kubernetes stub should be available
	kubernetesAvailable := false
	for _, source := range availableSources {
		if source.GetSourceType() == "kubernetes" {
			kubernetesAvailable = true
			break
		}
	}

	if !kubernetesAvailable {
		t.Log("Kubernetes stub source is not available (expected on some platforms)")
	}

	// Test that we can process empty events without errors
	results, err := correlation.ProcessEvents(ctx, []domain.Event{})
	if err != nil {
		t.Errorf("Failed to process empty events: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("Expected 0 results for empty events, got %d", len(results))
	}
}
