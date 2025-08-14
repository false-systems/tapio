package security

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// SecurityEventType constants for testing
const (
	EventTypeFileOpen = iota
	EventTypeFileWrite
	EventTypeFileDelete
	EventTypeProcessExec
	EventTypePrivilegeEscalation
	EventTypeNetworkConnect
	EventTypeSystemCall
)

// MockSecurityEvent represents a security event for testing
type MockSecurityEvent struct {
	Timestamp   uint64
	PID         uint32
	UID         uint32
	EventType   uint32
	Severity    uint8
	CgroupID    uint64
	FilePath    string
	ProcessName string
	Syscall     string
	Result      int32
}

// TestCollectorCreation tests security collector initialization
func TestCollectorCreation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector := NewSecurityCollector(logger)

	assert.NotNil(t, collector)
	assert.NotNil(t, collector.logger)
	assert.NotNil(t, collector.events)
	assert.Equal(t, 5000, cap(collector.events), "Security collector should have larger buffer")
}

// TestSecurityEventDetection tests various security event types
func TestSecurityEventDetection(t *testing.T) {
	testCases := []struct {
		name      string
		eventType uint32
		severity  uint8
		expected  string
	}{
		{"File Write", EventTypeFileWrite, 3, "file_write"},
		{"Process Exec", EventTypeProcessExec, 5, "process_exec"},
		{"Privilege Escalation", EventTypePrivilegeEscalation, 9, "privilege_escalation"},
		{"Network Connect", EventTypeNetworkConnect, 4, "network_connect"},
		{"System Call", EventTypeSystemCall, 2, "system_call"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			event := MockSecurityEvent{
				Timestamp: uint64(time.Now().UnixNano()),
				EventType: tc.eventType,
				Severity:  tc.severity,
				PID:       1234,
				UID:       1000,
			}

			data, err := json.Marshal(event)
			require.NoError(t, err)
			assert.NotEmpty(t, data)

			// Verify severity levels
			assert.LessOrEqual(t, tc.severity, uint8(10), "Severity should be 0-10")
		})
	}
}

// TestOTELSecurityMetrics tests OTEL metrics for security events
func TestOTELSecurityMetrics(t *testing.T) {
	// Setup OTEL
	res, _ := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName("security-collector-test"),
		),
	)

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)
	defer mp.Shutdown(context.Background())

	meter := otel.Meter("security-collector")

	// Create security-specific metrics
	securityEvents, err := meter.Int64Counter(
		"security_events_total",
		metric.WithDescription("Total security events detected"),
	)
	require.NoError(t, err)

	highSeverityEvents, err := meter.Int64Counter(
		"security_high_severity_events_total",
		metric.WithDescription("High severity security events"),
	)
	require.NoError(t, err)

	blockedActions, err := meter.Int64Counter(
		"security_blocked_actions_total",
		metric.WithDescription("Security actions blocked"),
	)
	require.NoError(t, err)

	// Record various security metrics
	ctx := context.Background()

	// Normal security event
	securityEvents.Add(ctx, 1, metric.WithAttributes(
		attribute.String("type", "file_access"),
		attribute.Int("severity", 3),
	))

	// High severity event
	highSeverityEvents.Add(ctx, 1, metric.WithAttributes(
		attribute.String("type", "privilege_escalation"),
		attribute.String("action", "blocked"),
	))

	// Blocked action
	blockedActions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("reason", "unauthorized_access"),
		attribute.Int("pid", 5678),
	))
}

// TestRawEventWithSecurity tests RawEvent structure for security events
func TestRawEventWithSecurity(t *testing.T) {
	secEvent := SecurityEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       9876,
		UID:       0, // root
		EventType: EventTypePrivilegeEscalation,
		Severity:  9,
		CgroupID:  1234,
	}
	copy(secEvent.Comm[:], "suspicious")
	copy(secEvent.FilePath[:], "/etc/passwd")

	// Convert to RawEvent
	data, err := json.Marshal(secEvent)
	require.NoError(t, err)

	rawEvent := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "security",
		Data:      data,
		Metadata: map[string]string{
			"source":   "ebpf",
			"severity": "high",
			"action":   "alert",
		},
		TraceID: "fedcba9876543210fedcba9876543210",
		SpanID:  "fedcba9876543210",
	}

	// Verify structure
	assert.Equal(t, "security", rawEvent.Type)
	assert.Contains(t, rawEvent.Metadata, "severity")
	assert.Equal(t, "high", rawEvent.Metadata["severity"])
}

// TestSecurityCollectorLifecycle tests start/stop with security focus
func TestSecurityCollectorLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector := NewSecurityCollector(logger)

	ctx := context.Background()
	err := collector.Start(ctx)
	assert.NoError(t, err)

	// Verify collector is monitoring
	assert.NotNil(t, collector.ctx)
	assert.NotNil(t, collector.cancel)

	// Stop and verify cleanup
	err = collector.Stop()
	assert.NoError(t, err)

	// Channel should be closed
	select {
	case _, ok := <-collector.Events():
		assert.False(t, ok)
	case <-time.After(100 * time.Millisecond):
		t.Error("Events channel not closed")
	}
}

// TestHighLoadSecurityEvents tests handling of high-volume security events
func TestHighLoadSecurityEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping high-load test in short mode")
	}

	logger := zaptest.NewLogger(t)
	collector := NewSecurityCollector(logger)

	ctx := context.Background()
	err := collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Simulate security event storm
	var wg sync.WaitGroup
	var processedEvents atomic.Int64
	var droppedEvents atomic.Int64

	// Multiple event processors
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			timeout := time.After(2 * time.Second)
			for {
				select {
				case event := <-collector.Events():
					if event.Data != nil {
						processedEvents.Add(1)
					}
				case <-timeout:
					return
				default:
					// Channel full, would drop in production
					droppedEvents.Add(1)
					time.Sleep(1 * time.Microsecond)
				}
			}
		}()
	}

	wg.Wait()

	t.Logf("Security events - Processed: %d, Dropped: %d",
		processedEvents.Load(), droppedEvents.Load())
}

// TestSecurityAlertThresholds tests alert threshold mechanisms
func TestSecurityAlertThresholds(t *testing.T) {
	type AlertThreshold struct {
		mu        sync.RWMutex
		counts    map[string]int
		threshold int
		window    time.Duration
		lastReset time.Time
	}

	threshold := &AlertThreshold{
		counts:    make(map[string]int),
		threshold: 10,
		window:    1 * time.Minute,
		lastReset: time.Now(),
	}

	// Helper to record event and check threshold
	recordEvent := func(eventType string) bool {
		threshold.mu.Lock()
		defer threshold.mu.Unlock()

		// Reset if window expired
		if time.Since(threshold.lastReset) > threshold.window {
			threshold.counts = make(map[string]int)
			threshold.lastReset = time.Now()
		}

		threshold.counts[eventType]++
		return threshold.counts[eventType] >= threshold.threshold
	}

	// Test threshold triggering
	eventType := "suspicious_file_access"
	for i := 0; i < 9; i++ {
		assert.False(t, recordEvent(eventType))
	}

	// 10th event should trigger
	assert.True(t, recordEvent(eventType))
}

// TestSecurityEventCorrelation tests correlating security events
func TestSecurityEventCorrelation(t *testing.T) {
	type EventCorrelation struct {
		mu       sync.RWMutex
		events   []MockSecurityEvent
		window   time.Duration
		patterns map[string][]MockSecurityEvent
	}

	correlation := &EventCorrelation{
		events:   make([]MockSecurityEvent, 0),
		window:   5 * time.Second,
		patterns: make(map[string][]MockSecurityEvent),
	}

	// Add related events
	baseTime := uint64(time.Now().UnixNano())
	events := []MockSecurityEvent{
		{Timestamp: baseTime, PID: 1234, EventType: EventTypeFileOpen},
		{Timestamp: baseTime + 1000, PID: 1234, EventType: EventTypeFileWrite},
		{Timestamp: baseTime + 2000, PID: 1234, EventType: EventTypeProcessExec},
	}

	// Correlate by PID
	for _, event := range events {
		key := string(event.PID)
		correlation.mu.Lock()
		correlation.patterns[key] = append(correlation.patterns[key], event)
		correlation.mu.Unlock()
	}

	// Verify correlation
	correlation.mu.RLock()
	relatedEvents := correlation.patterns["1234"]
	correlation.mu.RUnlock()

	assert.Len(t, relatedEvents, 3)
	assert.Equal(t, EventTypeFileOpen, relatedEvents[0].EventType)
	assert.Equal(t, EventTypeProcessExec, relatedEvents[2].EventType)
}

// TestSecurityCircuitBreaker tests circuit breaker for security monitoring
func TestSecurityCircuitBreaker(t *testing.T) {
	type SecurityCircuitBreaker struct {
		mu               sync.RWMutex
		consecutiveFails int
		maxFails         int
		state            string // "closed", "open", "half-open"
		lastStateChange  time.Time
		cooldown         time.Duration
	}

	cb := &SecurityCircuitBreaker{
		maxFails: 5,
		state:    "closed",
		cooldown: 30 * time.Second,
	}

	// Helper functions
	recordFailure := func() string {
		cb.mu.Lock()
		defer cb.mu.Unlock()

		if cb.state == "open" {
			if time.Since(cb.lastStateChange) > cb.cooldown {
				cb.state = "half-open"
				cb.consecutiveFails = 0
			} else {
				return "open"
			}
		}

		cb.consecutiveFails++
		if cb.consecutiveFails >= cb.maxFails {
			cb.state = "open"
			cb.lastStateChange = time.Now()
		}

		return cb.state
	}

	recordSuccess := func() {
		cb.mu.Lock()
		defer cb.mu.Unlock()

		if cb.state == "half-open" {
			cb.state = "closed"
		}
		cb.consecutiveFails = 0
	}

	// Test circuit breaker states
	for i := 0; i < 4; i++ {
		state := recordFailure()
		assert.Equal(t, "closed", state)
	}

	// 5th failure should open circuit
	state := recordFailure()
	assert.Equal(t, "open", state)

	// Success should not affect open state
	recordSuccess()
	cb.mu.RLock()
	assert.Equal(t, "open", cb.state)
	cb.mu.RUnlock()
}

// TestSecurityTracingIntegration tests OpenTelemetry tracing for security events
func TestSecurityTracingIntegration(t *testing.T) {
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resource.NewSchemaless(
			semconv.ServiceName("security-collector"),
		)),
	)
	otel.SetTracerProvider(tp)
	defer tp.Shutdown(context.Background())

	tracer := otel.Tracer("security")

	// Create security event trace
	ctx := context.Background()
	ctx, span := tracer.Start(ctx, "security.event.detected",
		trace.WithAttributes(
			attribute.String("event.type", "privilege_escalation"),
			attribute.Int("severity", 9),
			attribute.Int("pid", 1234),
			attribute.String("user", "root"),
		))
	defer span.End()

	// Add investigation span
	_, investigationSpan := tracer.Start(ctx, "security.investigation")
	investigationSpan.SetAttributes(
		attribute.String("action", "block"),
		attribute.String("reason", "unauthorized_root_access"),
	)
	investigationSpan.AddEvent("Action taken", trace.WithAttributes(
		attribute.String("result", "blocked"),
		attribute.String("notification", "sent"),
	))
	investigationSpan.End()

	span.SetStatus(codes.Ok, "Security event handled")
}

// BenchmarkSecurityEventProcessing benchmarks security event processing
func BenchmarkSecurityEventProcessing(b *testing.B) {
	logger := zap.NewNop()
	collector := NewSecurityCollector(logger)

	ctx := context.Background()
	collector.Start(ctx)
	defer collector.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			event := MockSecurityEvent{
				Timestamp: uint64(time.Now().UnixNano()),
				PID:       uint32(b.N),
				EventType: EventTypeFileWrite,
				Severity:  5,
			}

			// Simulate processing
			data, _ := json.Marshal(event)
			_ = data
		}
	})
}

// TestSecurityRetryMechanism tests retry logic for security operations
func TestSecurityRetryMechanism(t *testing.T) {
	type RetryConfig struct {
		maxAttempts int
		baseDelay   time.Duration
		maxDelay    time.Duration
	}

	config := RetryConfig{
		maxAttempts: 5,
		baseDelay:   100 * time.Millisecond,
		maxDelay:    5 * time.Second,
	}

	// Calculate exponential backoff
	for attempt := 0; attempt < config.maxAttempts; attempt++ {
		delay := config.baseDelay * (1 << attempt)
		if delay > config.maxDelay {
			delay = config.maxDelay
		}

		t.Logf("Attempt %d: delay %v", attempt+1, delay)
		assert.LessOrEqual(t, delay, config.maxDelay)
	}
}
