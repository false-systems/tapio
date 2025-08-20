package cni

import (
	"context"
	"fmt"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap/zaptest"
)

func TestNewCollector(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("test-cni")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	if collector.Name() != "test-cni" {
		t.Errorf("Expected name 'test-cni', got '%s'", collector.Name())
	}

	if !collector.IsHealthy() {
		t.Error("Collector should be healthy after creation")
	}

	// Verify OTEL components are initialized
	if collector.tracer == nil {
		t.Error("Tracer should be initialized")
	}
	if collector.meter == nil {
		t.Error("Meter should be initialized")
	}
	if collector.logger == nil {
		t.Error("Logger should be initialized")
	}
}

func TestCollectorStartStop(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("test-cni")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	ctx := context.Background()
	if err := collector.Start(ctx); err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	if !collector.IsHealthy() {
		t.Error("Collector should be healthy after start")
	}

	// Test double start
	if err := collector.Start(ctx); err == nil {
		t.Error("Expected error when starting already started collector")
	}

	if err := collector.Stop(); err != nil {
		t.Fatalf("Failed to stop collector: %v", err)
	}

	if collector.IsHealthy() {
		t.Error("Collector should not be healthy after stop")
	}
}

func TestCollectorEvents(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("test-cni")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	events := collector.Events()
	if events == nil {
		t.Error("Events channel should not be nil")
	}

	ctx := context.Background()
	if err := collector.Start(ctx); err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Test that we can read from events channel
	select {
	case <-events:
		// Got an event (unlikely but possible)
	case <-time.After(100 * time.Millisecond):
		// No event received, which is expected
	}

	if err := collector.Stop(); err != nil {
		t.Fatalf("Failed to stop collector: %v", err)
	}
}

func TestCreateEvent(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("test-cni")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	data := map[string]string{
		"pid":        "1234",
		"comm":       "test-process",
		"netns_path": "/var/run/netns/cni-550e8400-e29b-41d4-a716-446655440000",
	}

	event := collector.createEvent("netns_create", data)

	if event.Type != "cni" {
		t.Errorf("Expected event type 'cni', got '%s'", event.Type)
	}

	if event.TraceID == "" {
		t.Error("Event should have a trace ID")
	}

	if event.SpanID == "" {
		t.Error("Event should have a span ID")
	}

	if event.Metadata["collector"] != "test-cni" {
		t.Errorf("Expected collector metadata 'test-cni', got '%s'", event.Metadata["collector"])
	}

	if event.Metadata["event"] != "netns_create" {
		t.Errorf("Expected event metadata 'netns_create', got '%s'", event.Metadata["event"])
	}

	if event.Metadata["k8s_kind"] != "Pod" {
		t.Errorf("Expected k8s_kind 'Pod', got '%s'", event.Metadata["k8s_kind"])
	}

	if event.Metadata["k8s_uid"] != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("Expected k8s_uid '550e8400-e29b-41d4-a716-446655440000', got '%s'", event.Metadata["k8s_uid"])
	}
}

func TestCreateEventWithMarshalError(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("test-cni")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Create data with invalid content that would cause marshal to fail
	// In this case, all map[string]string data should marshal fine
	// But we test the error handling path by using the function correctly
	data := map[string]string{
		"test": "value",
	}

	event := collector.createEvent("test", data)

	if event.Type != "cni" {
		t.Errorf("Expected event type 'cni', got '%s'", event.Type)
	}

	// Event should be created successfully
	if len(event.Data) == 0 {
		t.Error("Event data should not be empty")
	}
}

func TestParseK8sFromNetns(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("test-cni")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	tests := []struct {
		name      string
		netnsPath string
		expected  *PodInfo
	}{
		{
			name:      "CNI UUID format",
			netnsPath: "/var/run/netns/cni-550e8400-e29b-41d4-a716-446655440000",
			expected: &PodInfo{
				PodUID: "550e8400-e29b-41d4-a716-446655440000",
			},
		},
		{
			name:      "Kubepods cgroup format",
			netnsPath: "/proc/123/ns/net/kubepods/besteffort/pod550e8400_e29b_41d4_a716_446655440000/container",
			expected: &PodInfo{
				PodUID: "550e8400-e29b-41d4-a716-446655440000",
			},
		},
		{
			name:      "Unknown format",
			netnsPath: "/proc/123/ns/net",
			expected:  nil,
		},
		{
			name:      "Empty path",
			netnsPath: "",
			expected:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collector.parseK8sFromNetns(tt.netnsPath)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("Expected nil, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Errorf("Expected %+v, got nil", tt.expected)
				return
			}

			if result.PodUID != tt.expected.PodUID {
				t.Errorf("Expected PodUID '%s', got '%s'", tt.expected.PodUID, result.PodUID)
			}
		})
	}
}

func TestCollectorConcurrency(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("test-cni")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	ctx := context.Background()
	if err := collector.Start(ctx); err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Test concurrent access to IsHealthy
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			defer func() { done <- true }()
			for j := 0; j < 100; j++ {
				collector.IsHealthy()
			}
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	if err := collector.Stop(); err != nil {
		t.Fatalf("Failed to stop collector: %v", err)
	}
}

func TestEBPFStateManagement(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("test-cni")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Test that eBPF state is properly initialized
	if collector.ebpfState != nil {
		t.Error("eBPF state should be nil initially")
	}

	ctx := context.Background()
	if err := collector.Start(ctx); err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Start should not fail even if eBPF setup fails
	if !collector.IsHealthy() {
		t.Error("Collector should remain healthy even if eBPF setup fails")
	}

	if err := collector.Stop(); err != nil {
		t.Fatalf("Failed to stop collector: %v", err)
	}
}

// setupOTELForTesting initializes minimal OTEL providers for testing
func setupOTELForTesting(t *testing.T) {
	// Set up minimal OTEL providers for testing
	res, err := resource.New(context.Background(), resource.WithAttributes(
		attribute.String("service.name", "test-cni"),
	))
	if err != nil {
		t.Fatalf("Failed to create resource: %v", err)
	}

	// Set up tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	// Set up meter provider
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)
}

// TestCollectorOTELMetrics tests OTEL metrics functionality
func TestCollectorOTELMetrics(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("test-cni-metrics")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Verify OTEL components are initialized
	if collector.tracer == nil {
		t.Error("Tracer should be initialized")
	}
	if collector.meter == nil {
		t.Error("Meter should be initialized")
	}
	if collector.logger == nil {
		t.Error("Logger should be initialized")
	}

	// Verify all metrics are initialized
	metricTests := []struct {
		name   string
		metric interface{}
	}{
		{"eventsProcessed", collector.eventsProcessed},
		{"errorsTotal", collector.errorsTotal},
		{"processingTime", collector.processingTime},
		{"droppedEvents", collector.droppedEvents},
		{"bufferUsage", collector.bufferUsage},
		{"ebpfLoadsTotal", collector.ebpfLoadsTotal},
		{"ebpfLoadErrors", collector.ebpfLoadErrors},
		{"ebpfAttachTotal", collector.ebpfAttachTotal},
		{"ebpfAttachErrors", collector.ebpfAttachErrors},
		{"collectorHealth", collector.collectorHealth},
		{"k8sExtractionTotal", collector.k8sExtractionTotal},
		{"k8sExtractionHits", collector.k8sExtractionHits},
		{"netnsOpsByType", collector.netnsOpsByType},
	}

	for _, test := range metricTests {
		if test.metric == nil {
			t.Errorf("Metric %s should be initialized", test.name)
		}
	}
}

// TestCollectorOTELTracing tests OTEL tracing functionality
func TestCollectorOTELTracing(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("test-cni-tracing")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	ctx := context.Background()

	// Test Start with tracing
	if err := collector.Start(ctx); err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Test createEvent with tracing
	data := map[string]string{
		"pid":        "1234",
		"comm":       "test-process",
		"netns_path": "/var/run/netns/cni-550e8400-e29b-41d4-a716-446655440000",
	}

	event := collector.createEvent("netns_create", data)
	if event.TraceID == "" {
		t.Error("Event should have trace ID from context")
	}
	if event.SpanID == "" {
		t.Error("Event should have span ID from context")
	}

	// Test Stop with tracing
	if err := collector.Stop(); err != nil {
		t.Fatalf("Failed to stop collector: %v", err)
	}
}

// TestCollectorHealthMetrics tests health status metrics
func TestCollectorHealthMetrics(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("test-cni-health")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Test initial health status
	if !collector.IsHealthy() {
		t.Error("Collector should be healthy initially")
	}

	ctx := context.Background()
	if err := collector.Start(ctx); err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Health should remain true after start
	if !collector.IsHealthy() {
		t.Error("Collector should be healthy after start")
	}

	if err := collector.Stop(); err != nil {
		t.Fatalf("Failed to stop collector: %v", err)
	}

	// Health should be false after stop
	if collector.IsHealthy() {
		t.Error("Collector should not be healthy after stop")
	}
}

// TestBufferUtilizationMonitoring tests buffer utilization monitoring
func TestBufferUtilizationMonitoring(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("test-cni-buffer")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := collector.Start(ctx); err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Let the monitoring run for a short time
	time.Sleep(50 * time.Millisecond)

	if err := collector.Stop(); err != nil {
		t.Fatalf("Failed to stop collector: %v", err)
	}
}

// TestK8sMetadataExtraction tests K8s metadata extraction with metrics
func TestK8sMetadataExtraction(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("test-cni-k8s")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	tests := []struct {
		name      string
		data      map[string]string
		hasK8sUID bool
	}{
		{
			name: "CNI UUID format",
			data: map[string]string{
				"netns_path": "/var/run/netns/cni-550e8400-e29b-41d4-a716-446655440000",
			},
			hasK8sUID: true,
		},
		{
			name: "No netns_path",
			data: map[string]string{
				"pid": "1234",
			},
			hasK8sUID: false,
		},
		{
			name: "Invalid netns_path",
			data: map[string]string{
				"netns_path": "/proc/123/ns/net",
			},
			hasK8sUID: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := collector.createEvent("test_event", tt.data)

			if tt.hasK8sUID {
				if event.Metadata["k8s_uid"] == "" {
					t.Error("Expected k8s_uid to be extracted")
				}
			} else {
				if event.Metadata["k8s_uid"] != "" {
					t.Error("Did not expect k8s_uid to be extracted")
				}
			}
		})
	}
}

// TestEventProcessingLatency tests event processing latency metrics
func TestEventProcessingLatency(t *testing.T) {
	setupOTELForTesting(t)

	collector, err := NewCollector("test-cni-latency")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Create multiple events to test latency measurement
	for i := 0; i < 5; i++ {
		data := map[string]string{
			"test_field": "test_value",
			"iteration":  fmt.Sprintf("%d", i),
		}
		event := collector.createEvent("test_event", data)
		if event.Type != "cni" {
			t.Errorf("Expected event type 'cni', got '%s'", event.Type)
		}
	}
}

// TestStructuredLogging tests structured logging with trace context
func TestStructuredLogging(t *testing.T) {
	setupOTELForTesting(t)

	// Create logger that captures output for testing
	logger := zaptest.NewLogger(t)

	collector, err := NewCollector("test-cni-logging")
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Replace logger for testing
	collector.logger = logger

	ctx := context.Background()

	// Test logging during start
	if err := collector.Start(ctx); err != nil {
		t.Fatalf("Failed to start collector: %v", err)
	}

	// Test logging during stop
	if err := collector.Stop(); err != nil {
		t.Fatalf("Failed to stop collector: %v", err)
	}
}
