package dataflow

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

// Example demonstrates how to integrate OTEL semantic correlation into Tapio's collector pipeline
func Example() {
	// Step 1: Initialize OTEL tracing
	ctx := context.Background()

	// Create OTLP exporter
	exporter, err := otlptrace.New(ctx, otlptracegrpc.NewClient(
		otlptracegrpc.WithEndpoint("localhost:4317"),
		otlptracegrpc.WithInsecure(),
	))
	if err != nil {
		log.Fatalf("Failed to create OTLP exporter: %v", err)
	}

	// Create trace provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String("tapio-collector"),
			semconv.ServiceVersionKey.String("2.0.0"),
			semconv.DeploymentEnvironmentKey.String("production"),
		)),
	)
	otel.SetTracerProvider(tp)
	defer tp.Shutdown(ctx)

	// Step 2: Create channels for event flow
	inputEvents := make(chan domain.Event, 1000)
	outputEvents := make(chan domain.Event, 1000)

	// Step 3: Create and configure TapioDataFlow
	dataFlowConfig := Config{
		EnableSemanticGrouping: true,
		GroupRetentionPeriod:   30 * time.Minute,
		ServiceName:            "tapio-collector",
		ServiceVersion:         "2.0.0",
		Environment:            "production",
		BufferSize:             1000,
		FlushInterval:          time.Second,
	}

	dataFlow := NewTapioDataFlow(dataFlowConfig)
	dataFlow.Connect(inputEvents, outputEvents)

	// Step 4: Create ServerBridge for forwarding to Tapio server
	bridgeConfig := BridgeConfig{
		ServerAddress: "localhost:9090",
		BufferSize:    500,
		FlushInterval: 2 * time.Second,
		MaxBatchSize:  100,
		EnableTracing: true,
	}

	bridge, err := NewServerBridge(bridgeConfig, dataFlow)
	if err != nil {
		log.Fatalf("Failed to create server bridge: %v", err)
	}

	// Step 5: Start all components
	if err := dataFlow.Start(); err != nil {
		log.Fatalf("Failed to start data flow: %v", err)
	}

	if err := bridge.Start(); err != nil {
		log.Fatalf("Failed to start server bridge: %v", err)
	}

	// Step 6: Process enriched events
	go func() {
		for event := range outputEvents {
			// Events here are enriched with:
			// - Semantic correlation metadata
			// - OTEL trace context
			// - Impact assessments
			// - Predictions

			fmt.Printf("Enriched event: %s\n", event.ID)

			// Check if event has correlation data
			if metadata := event.Context.Metadata; metadata != nil {
				if correlationID, ok := metadata["correlation_id"].(string); ok {
					fmt.Printf("  Correlation ID: %s\n", correlationID)
				}
				if semanticIntent, ok := metadata["semantic_intent"].(string); ok {
					fmt.Printf("  Semantic Intent: %s\n", semanticIntent)
				}
				if impactBusiness, ok := metadata["impact_business"].(float32); ok {
					fmt.Printf("  Business Impact: %.2f\n", impactBusiness)
				}
			}

			// Forward semantic findings to server
			if finding := extractFindingFromEvent(&event); finding != nil {
				bridge.SendFinding(finding, dataFlow.rootSpan.SpanContext())
			}
		}
	}()

	// Step 7: Simulate collector sending events
	go simulateCollectorEvents(inputEvents)

	// Step 8: Monitor metrics
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		metrics := dataFlow.GetMetrics()
		fmt.Printf("\n=== Data Flow Metrics ===\n")
		fmt.Printf("Events Processed: %d\n", metrics["events_processed"])
		fmt.Printf("Semantic Groups: %d\n", metrics["semantic_groups_active"])
		fmt.Printf("Events/Second: %.2f\n", metrics["events_per_second"])
		fmt.Printf("========================\n")
	}
}

// simulateCollectorEvents generates sample events for testing
func simulateCollectorEvents(events chan<- domain.Event) {
	// Simulate a memory pressure cascade scenario
	baseTime := time.Now()

	// Initial memory pressure event
	events <- domain.Event{
		ID:         "evt-001",
		Type:       "memory_pressure",
		Severity:   "high",
		Timestamp:  baseTime,
		Source:     "ebpf",
		Confidence: 0.95,
		Context: domain.EventContext{
			Namespace: "production",
			Host:      "node-1",
			Labels: map[string]string{
				"pod":        "api-server-abc123",
				"deployment": "api-server",
			},
		},
		Payload: domain.MemoryEventPayload{
			Usage:     95.5,
			Available: 200 * 1024 * 1024,      // 200MB
			Total:     4 * 1024 * 1024 * 1024, // 4GB
		},
	}

	// Related OOM kill event
	time.Sleep(2 * time.Second)
	events <- domain.Event{
		ID:         "evt-002",
		Type:       "memory_oom",
		Severity:   "critical",
		Timestamp:  baseTime.Add(2 * time.Second),
		Source:     "kernel",
		Confidence: 1.0,
		Context: domain.EventContext{
			Namespace: "production",
			Host:      "node-1",
			Labels: map[string]string{
				"pod":        "api-server-abc123",
				"deployment": "api-server",
			},
		},
		Payload: domain.SystemEventPayload{
			Message: "Out of memory: Kill process 12345 (java) score 950 or sacrifice child",
		},
	}

	// Pod eviction event
	time.Sleep(1 * time.Second)
	events <- domain.Event{
		ID:         "evt-003",
		Type:       "pod_evicted",
		Severity:   "high",
		Timestamp:  baseTime.Add(3 * time.Second),
		Source:     "kubernetes",
		Confidence: 1.0,
		Context: domain.EventContext{
			Namespace: "production",
			Host:      "node-1",
			Labels: map[string]string{
				"pod":        "api-server-abc123",
				"deployment": "api-server",
			},
		},
		Payload: domain.KubernetesEventPayload{
			Resource: domain.ResourceInfo{
				Kind:      "Pod",
				Name:      "api-server-abc123",
				Namespace: "production",
			},
			EventType: "Evicted",
			Reason:    "OOMKilled",
			Message:   "The node was low on resource: memory",
		},
	}

	// Service restart event
	time.Sleep(2 * time.Second)
	events <- domain.Event{
		ID:         "evt-004",
		Type:       "service_restart",
		Severity:   "medium",
		Timestamp:  baseTime.Add(5 * time.Second),
		Source:     "systemd",
		Confidence: 0.9,
		Context: domain.EventContext{
			Namespace: "production",
			Host:      "node-1",
			Labels: map[string]string{
				"service": "api-server",
			},
		},
		Payload: domain.ServiceEventPayload{
			ServiceName: "api-server",
			EventType:   "restart",
			Message:     "Service restarted after failure",
		},
	}

	// Continue generating events periodically
	ticker := time.NewTicker(10 * time.Second)
	eventID := 5

	for range ticker.C {
		// Generate various event types
		switch eventID % 4 {
		case 0:
			events <- generateNetworkEvent(eventID, baseTime.Add(time.Duration(eventID)*time.Second))
		case 1:
			events <- generateCPUEvent(eventID, baseTime.Add(time.Duration(eventID)*time.Second))
		case 2:
			events <- generateDiskEvent(eventID, baseTime.Add(time.Duration(eventID)*time.Second))
		case 3:
			events <- generateServiceEvent(eventID, baseTime.Add(time.Duration(eventID)*time.Second))
		}
		eventID++
	}
}

// Helper functions for generating different event types

func generateNetworkEvent(id int, timestamp time.Time) domain.Event {
	return domain.Event{
		ID:         domain.EventID(fmt.Sprintf("evt-%03d", id)),
		Type:       "network_timeout",
		Severity:   "medium",
		Timestamp:  timestamp,
		Source:     "ebpf",
		Confidence: 0.85,
		Context: domain.EventContext{
			Namespace: "production",
			Host:      "node-2",
			Labels: map[string]string{
				"pod":        "frontend-xyz789",
				"deployment": "frontend",
			},
		},
		Payload: domain.NetworkEventPayload{
			Protocol:    "tcp",
			Source:      "10.0.0.1:8080",
			Destination: "10.0.0.2:5432",
			Latency:     5000, // 5s timeout
		},
	}
}

func generateCPUEvent(id int, timestamp time.Time) domain.Event {
	return domain.Event{
		ID:         domain.EventID(fmt.Sprintf("evt-%03d", id)),
		Type:       "cpu_throttling",
		Severity:   "low",
		Timestamp:  timestamp,
		Source:     "cgroup",
		Confidence: 0.9,
		Context: domain.EventContext{
			Namespace: "production",
			Host:      "node-3",
			Labels: map[string]string{
				"pod":        "worker-def456",
				"deployment": "worker",
			},
		},
		Payload: domain.CPUEventPayload{
			Usage:            85.0,
			ThrottledTime:    1000000000, // 1s
			ThrottledPeriods: 50,
		},
	}
}

func generateDiskEvent(id int, timestamp time.Time) domain.Event {
	return domain.Event{
		ID:         domain.EventID(fmt.Sprintf("evt-%03d", id)),
		Type:       "disk_pressure",
		Severity:   "medium",
		Timestamp:  timestamp,
		Source:     "node_exporter",
		Confidence: 0.95,
		Context: domain.EventContext{
			Namespace: "production",
			Host:      "node-1",
			Labels: map[string]string{
				"device": "/dev/sda1",
				"mount":  "/var/lib/docker",
			},
		},
		Payload: domain.DiskEventPayload{
			Device:    "/dev/sda1",
			Usage:     90.5,
			Available: 5 * 1024 * 1024 * 1024,  // 5GB
			Total:     50 * 1024 * 1024 * 1024, // 50GB
		},
	}
}

func generateServiceEvent(id int, timestamp time.Time) domain.Event {
	return domain.Event{
		ID:         domain.EventID(fmt.Sprintf("evt-%03d", id)),
		Type:       "service_failure",
		Severity:   "high",
		Timestamp:  timestamp,
		Source:     "systemd",
		Confidence: 1.0,
		Context: domain.EventContext{
			Namespace: "production",
			Host:      "node-2",
			Labels: map[string]string{
				"service": "database",
			},
		},
		Payload: domain.ServiceEventPayload{
			ServiceName: "postgresql",
			EventType:   "failure",
			Message:     "Main process exited, code=exited, status=1/FAILURE",
		},
	}
}

// extractFindingFromEvent extracts correlation finding from enriched event metadata
func extractFindingFromEvent(event *domain.Event) *correlation.Finding {
	if event.Context.Metadata == nil {
		return nil
	}

	// Check if event has correlation data
	correlationID, hasCorrelation := event.Context.Metadata["correlation_id"].(string)
	if !hasCorrelation {
		return nil
	}

	// Build finding from metadata
	finding := &correlation.Finding{
		ID:            correlationID,
		PatternType:   getStringMetadata(event, "correlation_pattern"),
		Confidence:    getFloatMetadata(event, "correlation_confidence"),
		RelatedEvents: []*domain.Event{event}, // In real scenario, would include all related events
		Timestamp:     event.Timestamp,
		Description:   fmt.Sprintf("Semantic correlation for %s", event.Type),
	}

	// Add semantic group info if available
	if semanticGroupID := getStringMetadata(event, "semantic_group_id"); semanticGroupID != "" {
		finding.SemanticGroup = &correlation.SemanticGroupSummary{
			ID:     semanticGroupID,
			Intent: getStringMetadata(event, "semantic_intent"),
			Type:   getStringMetadata(event, "semantic_type"),
		}

		// Add impact if available
		if businessImpact := getFloatMetadata(event, "impact_business"); businessImpact > 0 {
			finding.SemanticGroup.Impact = &correlation.ImpactAssessment{
				BusinessImpact:    float32(businessImpact),
				CascadeRisk:       float32(getFloatMetadata(event, "impact_cascade_risk")),
				TechnicalSeverity: getStringMetadata(event, "impact_severity"),
			}
		}

		// Add prediction if available
		if scenario := getStringMetadata(event, "prediction_scenario"); scenario != "" {
			finding.SemanticGroup.Prediction = &correlation.PredictedOutcome{
				Scenario:    scenario,
				Probability: getFloatMetadata(event, "prediction_probability"),
			}
		}
	}

	return finding
}

// Helper functions for metadata extraction

func getStringMetadata(event *domain.Event, key string) string {
	if val, ok := event.Context.Metadata[key].(string); ok {
		return val
	}
	return ""
}

func getFloatMetadata(event *domain.Event, key string) float64 {
	switch val := event.Context.Metadata[key].(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	default:
		return 0
	}
}
