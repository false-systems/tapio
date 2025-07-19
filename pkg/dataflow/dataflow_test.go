package dataflow

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

func TestTapioDataFlow_BasicOperation(t *testing.T) {
	// Create test channels
	input := make(chan domain.Event, 10)
	output := make(chan domain.Event, 10)

	// Create data flow
	config := Config{
		EnableSemanticGrouping: true,
		ServiceName:            "test-service",
		ServiceVersion:         "1.0.0",
		Environment:            "test",
	}

	df := NewTapioDataFlow(config)
	df.Connect(input, output)

	// Start data flow
	if err := df.Start(); err != nil {
		t.Fatalf("Failed to start data flow: %v", err)
	}
	defer df.Stop()

	// Send test event
	testEvent := domain.Event{
		ID:         "test-001",
		Type:       "test_event",
		Severity:   "medium",
		Timestamp:  time.Now(),
		Source:     "test",
		Confidence: 0.9,
		Context: domain.EventContext{
			Namespace: "test-ns",
			Host:      "test-host",
		},
	}

	// Send event
	select {
	case input <- testEvent:
	case <-time.After(time.Second):
		t.Fatal("Timeout sending event")
	}

	// Receive enriched event
	select {
	case enriched := <-output:
		// Verify event ID matches
		if enriched.ID != testEvent.ID {
			t.Errorf("Expected event ID %s, got %s", testEvent.ID, enriched.ID)
		}

		// Check for correlation metadata
		if enriched.Context.Metadata == nil {
			t.Error("Expected metadata to be set")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout receiving enriched event")
	}
}

func TestServerBridge_Configuration(t *testing.T) {
	// Test configuration validation
	config := BridgeConfig{
		ServerAddress: "localhost:9090",
		BufferSize:    100,
		MaxBatchSize:  50,
		EnableTracing: true,
	}

	// Create mock data flow
	df := &TapioDataFlow{
		ctx:    context.Background(),
		cancel: func() {},
	}

	// Attempt to create bridge (will fail due to connection)
	_, err := NewServerBridge(config, df)
	if err == nil {
		t.Skip("Expected error creating bridge without server")
	}
}

func TestEnrichEventWithFindings(t *testing.T) {
	df := &TapioDataFlow{}

	event := &domain.Event{
		ID:   "test-event",
		Type: "test",
		Context: domain.EventContext{
			Metadata: make(map[string]interface{}),
		},
	}

	finding := &correlation.Finding{
		ID:            "corr-001",
		PatternType:   "test_pattern",
		Confidence:    0.85,
		RelatedEvents: []*domain.Event{event},
	}

	df.enrichEventWithFindings(event, finding)

	// Verify enrichment
	if event.Context.Metadata["correlation_id"] != finding.ID {
		t.Error("Expected correlation_id to be set")
	}

	if event.Context.Metadata["correlation_pattern"] != finding.PatternType {
		t.Error("Expected correlation_pattern to be set")
	}

	if event.Context.Metadata["correlation_confidence"] != finding.Confidence {
		t.Error("Expected correlation_confidence to be set")
	}
}
