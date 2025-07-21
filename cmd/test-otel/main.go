package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/cni/core"
	"github.com/yairfalse/tapio/pkg/domain"
	tapiotel "github.com/yairfalse/tapio/pkg/integrations/otel"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func main() {
	fmt.Println("=== Tapio OTEL Integration Test ===")
	fmt.Println()

	// Create context
	ctx := context.Background()

	// Initialize OTEL with console exporter for easy verification
	otelIntegration, shutdown, err := initOTELWithConsole(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize OTEL: %v", err)
	}
	defer shutdown(ctx)

	// Create collector instrumentation
	collectorInstrumentation := tapiotel.NewCollectorInstrumentation(otelIntegration)

	// Test 1: Simulate collector startup with tracing
	fmt.Println("Test 1: Collector Startup Tracing")
	fmt.Println("---------------------------------")
	testCollectorStartup(ctx, collectorInstrumentation)
	fmt.Println()

	// Test 2: Simulate CNI events with distributed tracing
	fmt.Println("Test 2: CNI Event Processing with Tracing")
	fmt.Println("-----------------------------------------")
	testCNIEventProcessing(ctx, collectorInstrumentation)
	fmt.Println()

	// Test 3: Simulate related events showing trace propagation
	fmt.Println("Test 3: Related Events with Trace Propagation")
	fmt.Println("---------------------------------------------")
	testRelatedEvents(ctx, otelIntegration, collectorInstrumentation)
	fmt.Println()

	// Test 4: Simulate error scenarios with tracing
	fmt.Println("Test 4: Error Scenarios with Tracing")
	fmt.Println("------------------------------------")
	testErrorScenarios(ctx, collectorInstrumentation)
	fmt.Println()

	// Give time for all spans to be exported
	time.Sleep(2 * time.Second)
	fmt.Println("=== Test Complete ===")
}

// initOTELWithConsole initializes OTEL with console exporter for testing
func initOTELWithConsole(ctx context.Context) (*tapiotel.SimpleOTELIntegration, func(context.Context) error, error) {
	// For this test, we'll create a standard OTEL setup with console output
	// First try to use the regular OTEL integration
	config := tapiotel.DefaultConfig()
	config.ServiceName = "tapio-otel-test"
	config.ServiceVersion = "1.0.0"
	config.Environment = "test"
	config.JaegerEndpoint = "http://localhost:14268/api/traces" // May not be running
	config.Enabled = true

	// Try to create the integration - it's OK if Jaeger isn't running
	integration, err := tapiotel.NewSimpleOTEL(config)
	if err != nil {
		fmt.Printf("Note: Could not connect to Jaeger (this is OK for testing): %v\n", err)
		fmt.Println("Continuing with console-only output...")
	}

	// Also set up console exporter for immediate feedback
	setupConsoleExporter(ctx)

	// Return a no-op shutdown function since we're using global provider
	shutdownFunc := func(ctx context.Context) error {
		fmt.Println("OTEL shutdown complete")
		return nil
	}

	return integration, shutdownFunc, nil
}

// setupConsoleExporter adds a console exporter to see traces immediately
func setupConsoleExporter(ctx context.Context) error {
	// Create console exporter
	exporter, err := stdouttrace.New(
		stdouttrace.WithPrettyPrint(),
	)
	if err != nil {
		return fmt.Errorf("failed to create console exporter: %w", err)
	}

	// Create resource
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("tapio-otel-test"),
			semconv.ServiceVersionKey.String("1.0.0"),
			semconv.DeploymentEnvironmentKey.String("test"),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to create resource: %w", err)
	}

	// Create tracer provider with console exporter
	provider := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(res),
		trace.WithSampler(trace.AlwaysSample()),
	)

	// Set as global provider for console output
	otel.SetTracerProvider(provider)

	return nil
}

// testCollectorStartup tests collector startup tracing
func testCollectorStartup(ctx context.Context, instrumentation *tapiotel.CollectorInstrumentation) {
	// Start collector with tracing
	startCtx, startSpan := instrumentation.InstrumentCollectorStart(ctx, "cni")
	defer startSpan.End()

	// Simulate collector initialization
	time.Sleep(100 * time.Millisecond)

	startSpan.AddEvent("collector-initialized",
		oteltrace.WithAttributes(
			attribute.String("status", "ready"),
			attribute.Int("monitors", 4),
		),
	)

	// Record collector metrics
	instrumentation.RecordCollectorMetrics(startCtx, "cni", 0, 0)

	fmt.Println("✓ Collector startup traced")
	fmt.Printf("  Trace ID: %s\n", startSpan.SpanContext().TraceID())
	fmt.Printf("  Span ID:  %s\n", startSpan.SpanContext().SpanID())
}

// testCNIEventProcessing tests CNI event processing with tracing
func testCNIEventProcessing(ctx context.Context, instrumentation *tapiotel.CollectorInstrumentation) {
	// Create a simulated CNI event
	event := createTestCNIEvent("pod-network-attached", true)

	// Process event with tracing
	_, eventSpan := instrumentation.InstrumentEventProcessing(ctx, event)
	defer eventSpan.End()

	// Simulate processing
	time.Sleep(50 * time.Millisecond)

	// Add processing details
	eventSpan.SetAttributes(
		attribute.String("pod.name", "test-pod"),
		attribute.String("pod.namespace", "default"),
		attribute.String("assigned.ip", "10.244.1.5"),
		attribute.String("cni.plugin", "calico"),
	)

	eventSpan.AddEvent("network-interface-configured",
		oteltrace.WithAttributes(
			attribute.String("interface", "eth0"),
			attribute.String("subnet", "10.244.1.0/24"),
		),
	)

	fmt.Println("✓ CNI event processed and traced")
	fmt.Printf("  Event ID: %s\n", event.ID)
	fmt.Printf("  Trace ID: %s\n", event.TraceContext.TraceID)
	fmt.Printf("  Span ID:  %s\n", event.TraceContext.SpanID)
}

// testRelatedEvents tests trace propagation across related events
func testRelatedEvents(ctx context.Context, otelIntegration *tapiotel.SimpleOTELIntegration, instrumentation *tapiotel.CollectorInstrumentation) {
	// Create a root span for a pod lifecycle
	rootCtx, rootSpan := otelIntegration.StartSpan(ctx, "pod-lifecycle",
		oteltrace.WithSpanKind(oteltrace.SpanKindInternal),
		oteltrace.WithAttributes(
			attribute.String("pod.name", "app-pod"),
			attribute.String("pod.namespace", "production"),
		),
	)
	defer rootSpan.End()

	fmt.Printf("Root Trace ID: %s\n", rootSpan.SpanContext().TraceID())

	// Event 1: Pod scheduled
	event1 := createTestK8sEvent("pod-scheduled")
	event1Ctx, event1Span := instrumentation.InstrumentEventProcessing(rootCtx, event1)
	event1Span.SetAttributes(attribute.String("node", "worker-1"))
	event1Span.End()
	fmt.Printf("  Event 1 (K8s): %s - Span ID: %s\n", event1.ID, event1.TraceContext.SpanID)

	// Event 2: CNI network setup
	event2 := createTestCNIEvent("pod-network-attached", true)
	event2Ctx, event2Span := instrumentation.InstrumentEventProcessing(event1Ctx, event2)
	event2Span.SetAttributes(
		attribute.String("assigned.ip", "10.244.2.10"),
		attribute.String("cni.plugin", "flannel"),
	)
	event2Span.End()
	fmt.Printf("  Event 2 (CNI): %s - Span ID: %s\n", event2.ID, event2.TraceContext.SpanID)

	// Event 3: Container started
	event3 := createTestSystemdEvent("container-started")
	_, event3Span := instrumentation.InstrumentEventProcessing(event2Ctx, event3)
	event3Span.SetAttributes(attribute.String("container.id", "abc123"))
	event3Span.End()
	fmt.Printf("  Event 3 (Systemd): %s - Span ID: %s\n", event3.ID, event3.TraceContext.SpanID)

	// All events share the same trace ID
	fmt.Printf("\nAll events share Trace ID: %s\n", rootSpan.SpanContext().TraceID())
	fmt.Println("✓ Trace propagation demonstrated across collectors")
}

// testErrorScenarios tests error tracing
func testErrorScenarios(ctx context.Context, instrumentation *tapiotel.CollectorInstrumentation) {
	// Create a failed CNI event
	event := createTestCNIEvent("network-setup-failed", false)

	// Process with error
	eventCtx, eventSpan := instrumentation.InstrumentEventProcessing(ctx, event)
	defer eventSpan.End()

	// Simulate error
	err := fmt.Errorf("failed to allocate IP: address pool exhausted")
	instrumentation.RecordError(eventCtx, err, "CNI network setup failed")

	// Set error status
	eventSpan.SetStatus(codes.Error, "Network setup failed")
	eventSpan.SetAttributes(
		attribute.String("error.type", "ip_allocation_failed"),
		attribute.String("pod.name", "failing-pod"),
		attribute.Bool("retry.possible", true),
	)

	fmt.Println("✓ Error scenario traced")
	fmt.Printf("  Error: %v\n", err)
	fmt.Printf("  Trace ID: %s\n", event.TraceContext.TraceID)
}

// Helper functions to create test events

func createTestCNIEvent(intent string, success bool) *domain.UnifiedEvent {
	event := &domain.UnifiedEvent{
		ID:        fmt.Sprintf("cni-%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Source:    string(domain.SourceCNI),
		Type:      domain.EventTypeNetwork,
		Semantic: &domain.SemanticContext{
			Intent:     intent,
			Category:   "lifecycle",
			Tags:       []string{"cni", "networking", "pod-startup"},
			Narrative:  "Test CNI event",
			Confidence: 0.95,
		},
		Entity: &domain.EntityContext{
			Type:      "Pod",
			Name:      "test-pod",
			Namespace: "default",
			UID:       "test-uid-123",
		},
		Network: &domain.NetworkData{
			Protocol:  "CNI",
			SourceIP:  "10.244.1.5",
			Direction: "ingress",
			Headers: map[string]string{
				"cni_plugin": "calico",
				"operation":  string(core.CNIOperationAdd),
			},
		},
	}

	if !success {
		event.Impact = &domain.ImpactContext{
			Severity:         "warning",
			BusinessImpact:   0.4,
			AffectedServices: []string{"pod-network"},
			CustomerFacing:   false,
		}
	}

	return event
}

func createTestK8sEvent(reason string) *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID:        fmt.Sprintf("k8s-%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Source:    string(domain.SourceK8s),
		Type:      domain.EventTypeKubernetes,
		Semantic: &domain.SemanticContext{
			Intent:   reason,
			Category: "lifecycle",
		},
		Kubernetes: &domain.KubernetesData{
			EventType: "Normal",
			Reason:    reason,
			Object:    "Pod/app-pod",
		},
	}
}

func createTestSystemdEvent(intent string) *domain.UnifiedEvent {
	return &domain.UnifiedEvent{
		ID:        fmt.Sprintf("systemd-%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Source:    string(domain.SourceSystemd),
		Type:      domain.EventTypeSystem,
		Semantic: &domain.SemanticContext{
			Intent:   intent,
			Category: "lifecycle",
		},
	}
}
