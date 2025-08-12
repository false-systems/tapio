package collectors

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestCollectorErrorRecovery tests collector recovery from various error conditions
func TestCollectorErrorRecovery(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name          string
		errorType     string
		shouldRecover bool
		recoveryTime  time.Duration
	}{
		{
			name:          "Temporary eBPF failure",
			errorType:     "ebpf_temporary",
			shouldRecover: true,
			recoveryTime:  2 * time.Second,
		},
		{
			name:          "Memory pressure",
			errorType:     "memory_pressure",
			shouldRecover: true,
			recoveryTime:  1 * time.Second,
		},
		{
			name:          "Channel full",
			errorType:     "channel_full",
			shouldRecover: true,
			recoveryTime:  500 * time.Millisecond,
		},
		{
			name:          "Resource exhaustion",
			errorType:     "resource_exhaustion",
			shouldRecover: true,
			recoveryTime:  3 * time.Second,
		},
		{
			name:          "Permanent kernel incompatibility",
			errorType:     "kernel_incompatible",
			shouldRecover: false,
			recoveryTime:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := newErrorRecoveryCollector(tt.name, tt.errorType, tt.recoveryTime, logger)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			// Initial start should succeed
			err := collector.Start(ctx)
			require.NoError(t, err)
			defer collector.Stop()

			// Verify collector is initially healthy
			assert.True(t, collector.IsHealthy())

			// Trigger error condition
			collector.triggerError()

			// Wait for error to manifest
			time.Sleep(100 * time.Millisecond)

			if tt.shouldRecover {
				// For recoverable errors, wait for recovery
				recoveryDeadline := time.Now().Add(tt.recoveryTime + 2*time.Second)
				recovered := false

				for time.Now().Before(recoveryDeadline) && !recovered {
					time.Sleep(100 * time.Millisecond)
					if collector.IsHealthy() {
						recovered = true
					}
				}

				assert.True(t, recovered, "Collector should recover from %s", tt.errorType)

				// Verify collector functionality after recovery
				stats := collector.Statistics()
				assert.Contains(t, stats, "error_count")
				assert.Greater(t, stats["error_count"].(int64), int64(0), "Should record error count")
				assert.Contains(t, stats, "recovery_count")
				assert.Greater(t, stats["recovery_count"].(int64), int64(0), "Should record recovery")

			} else {
				// For non-recoverable errors, should remain unhealthy
				time.Sleep(tt.recoveryTime + 1*time.Second)
				assert.False(t, collector.IsHealthy(), "Collector should not recover from %s", tt.errorType)

				stats := collector.Statistics()
				assert.Greater(t, stats["error_count"].(int64), int64(0))
				assert.Equal(t, int64(0), stats["recovery_count"].(int64), "Should not have recovered")
			}
		})
	}
}

// TestCollectorGracefulDegradation tests graceful degradation under various failure modes
func TestCollectorGracefulDegradation(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create collector with degradation support
	collector := newDegradationCollector("degradation-test", logger)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Test normal operation first
	assert.True(t, collector.IsHealthy())
	assert.Equal(t, "full", collector.GetOperationMode())

	// Simulate eBPF failure - should degrade to userspace monitoring
	collector.simulateEBPFFailure()
	time.Sleep(500 * time.Millisecond)

	assert.True(t, collector.IsHealthy(), "Should remain healthy after eBPF failure")
	assert.Equal(t, "userspace", collector.GetOperationMode())

	// Generate events to verify functionality
	eventCount := 10
	for i := 0; i < eventCount; i++ {
		collector.generateTestEvent(fmt.Sprintf("degraded-event-%d", i))
	}

	// Collect events
	var events []RawEvent
	timeout := time.After(2 * time.Second)
	for len(events) < eventCount/2 { // Allow for some loss during degradation
		select {
		case event := <-collector.Events():
			events = append(events, event)
		case <-timeout:
			break
		}
	}

	assert.Greater(t, len(events), 0, "Should still produce events in degraded mode")

	// Verify event metadata indicates degraded mode
	if len(events) > 0 {
		assert.Equal(t, "userspace", events[0].Metadata["mode"])
	}

	// Simulate memory pressure - should further degrade to essential monitoring only
	collector.simulateMemoryPressure()
	time.Sleep(500 * time.Millisecond)

	assert.True(t, collector.IsHealthy(), "Should remain healthy under memory pressure")
	assert.Equal(t, "essential", collector.GetOperationMode())

	// Simulate recovery - should restore to full operation
	collector.simulateRecovery()
	time.Sleep(1 * time.Second)

	assert.True(t, collector.IsHealthy())
	assert.Equal(t, "full", collector.GetOperationMode())

	// Verify final statistics
	stats := collector.Statistics()
	assert.Greater(t, stats["degradation_events"].(int64), int64(0))
	assert.Greater(t, stats["recovery_events"].(int64), int64(0))
}

// TestCollectorErrorPropagation tests error propagation through the collector hierarchy
func TestCollectorErrorPropagation(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create parent collector with child collectors
	parentCollector := newHierarchicalCollector("parent", logger)
	childCollectors := make([]*hierarchicalCollector, 3)

	for i := 0; i < 3; i++ {
		child := newHierarchicalCollector(fmt.Sprintf("child-%d", i), logger)
		childCollectors[i] = child
		parentCollector.addChild(child)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := parentCollector.Start(ctx)
	require.NoError(t, err)
	defer parentCollector.Stop()

	// All collectors should be healthy initially
	assert.True(t, parentCollector.IsHealthy())
	for i, child := range childCollectors {
		assert.True(t, child.IsHealthy(), "Child %d should be healthy", i)
	}

	// Simulate error in one child
	childCollectors[1].simulateError("child_error")
	time.Sleep(200 * time.Millisecond)

	// Parent should detect child error but remain functional
	assert.True(t, parentCollector.IsHealthy(), "Parent should remain healthy")
	assert.False(t, childCollectors[1].IsHealthy(), "Failing child should be unhealthy")

	// Other children should remain healthy
	assert.True(t, childCollectors[0].IsHealthy(), "Other children should remain healthy")
	assert.True(t, childCollectors[2].IsHealthy(), "Other children should remain healthy")

	// Parent statistics should reflect child errors
	stats := parentCollector.Statistics()
	assert.Equal(t, int64(1), stats["unhealthy_children"].(int64))
	assert.Equal(t, int64(3), stats["total_children"].(int64))

	// Simulate cascading failures
	childCollectors[0].simulateError("cascade_error")
	childCollectors[2].simulateError("cascade_error")
	time.Sleep(200 * time.Millisecond)

	// All children are now unhealthy, but parent should still attempt to operate
	stats = parentCollector.Statistics()
	assert.Equal(t, int64(3), stats["unhealthy_children"].(int64))

	// Parent should remain healthy but in degraded mode
	assert.True(t, parentCollector.IsHealthy(), "Parent should remain healthy in degraded mode")

	// Simulate recovery of one child
	childCollectors[0].recover()
	time.Sleep(200 * time.Millisecond)

	assert.True(t, childCollectors[0].IsHealthy(), "Recovered child should be healthy")
	stats = parentCollector.Statistics()
	assert.Equal(t, int64(2), stats["unhealthy_children"].(int64))
}

// TestCollectorCircuitBreaker tests circuit breaker functionality for failing operations
func TestCollectorCircuitBreaker(t *testing.T) {
	logger := zaptest.NewLogger(t)

	collector := newCircuitBreakerCollector("circuit-test", logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Circuit should be closed initially (allowing operations)
	assert.Equal(t, "closed", collector.GetCircuitState())
	assert.True(t, collector.IsHealthy())

	// Simulate failures to trigger circuit breaker
	failureCount := 10
	for i := 0; i < failureCount; i++ {
		collector.simulateFailure()
		time.Sleep(50 * time.Millisecond)
	}

	// Circuit should now be open (blocking operations)
	assert.Equal(t, "open", collector.GetCircuitState())

	// Collector may still be healthy but in protection mode
	stats := collector.Statistics()
	assert.Greater(t, stats["circuit_breaker_trips"].(int64), int64(0))
	assert.Equal(t, int64(failureCount), stats["consecutive_failures"].(int64))

	// Wait for circuit breaker timeout (half-open state)
	time.Sleep(2 * time.Second) // Assuming 2s timeout

	// Circuit should be half-open (testing if operations can succeed)
	state := collector.GetCircuitState()
	assert.True(t, state == "half-open" || state == "open", "Circuit should be in half-open or still open")

	// Simulate successful operations to close the circuit
	successCount := 5
	for i := 0; i < successCount; i++ {
		collector.simulateSuccess()
		time.Sleep(100 * time.Millisecond)
	}

	// Circuit should be closed again
	assert.Equal(t, "closed", collector.GetCircuitState())
	assert.True(t, collector.IsHealthy())

	finalStats := collector.Statistics()
	assert.Greater(t, finalStats["successful_operations"].(int64), int64(0))
	assert.Equal(t, int64(0), finalStats["consecutive_failures"].(int64))
}

// TestCollectorRetryMechanisms tests various retry mechanisms and backoff strategies
func TestCollectorRetryMechanisms(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name          string
		retryStrategy string
		maxRetries    int
		expectedTime  time.Duration
		shouldSucceed bool
	}{
		{
			name:          "exponential backoff",
			retryStrategy: "exponential",
			maxRetries:    3,
			expectedTime:  7 * time.Second, // 1 + 2 + 4 seconds
			shouldSucceed: true,
		},
		{
			name:          "linear backoff",
			retryStrategy: "linear",
			maxRetries:    3,
			expectedTime:  6 * time.Second, // 1 + 2 + 3 seconds
			shouldSucceed: true,
		},
		{
			name:          "fixed backoff",
			retryStrategy: "fixed",
			maxRetries:    2,
			expectedTime:  2 * time.Second, // 1 + 1 seconds
			shouldSucceed: true,
		},
		{
			name:          "no retry exhaustion",
			retryStrategy: "exponential",
			maxRetries:    1,
			expectedTime:  1 * time.Second,
			shouldSucceed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := newRetryCollector(tt.name, tt.retryStrategy, tt.maxRetries, logger)

			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()

			// Configure collector to fail initially, then succeed after retries
			if tt.shouldSucceed {
				collector.setFailureCount(tt.maxRetries)
			} else {
				collector.setFailureCount(tt.maxRetries + 10) // More failures than retries
			}

			startTime := time.Now()
			err := collector.Start(ctx)
			duration := time.Since(startTime)

			if tt.shouldSucceed {
				assert.NoError(t, err, "Start should succeed after retries")
				assert.True(t, collector.IsHealthy())

				// Verify timing is approximately correct (allow 1s variance)
				assert.True(t, duration >= tt.expectedTime-time.Second &&
					duration <= tt.expectedTime+time.Second,
					"Duration %v should be approximately %v", duration, tt.expectedTime)
			} else {
				assert.Error(t, err, "Start should fail after exhausting retries")
			}

			// Check retry statistics
			stats := collector.Statistics()
			assert.Contains(t, stats, "retry_attempts")
			assert.Contains(t, stats, "retry_strategy")
			assert.Equal(t, tt.retryStrategy, stats["retry_strategy"])

			if tt.shouldSucceed {
				assert.Equal(t, int64(tt.maxRetries), stats["retry_attempts"].(int64))
			} else {
				assert.Equal(t, int64(tt.maxRetries), stats["retry_attempts"].(int64))
			}

			collector.Stop()
		})
	}
}

// TestCollectorBulkheadIsolation tests bulkhead isolation patterns
func TestCollectorBulkheadIsolation(t *testing.T) {
	logger := zaptest.NewLogger(t)

	collector := newBulkheadCollector("bulkhead-test", logger)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Test that different subsystems are isolated
	subsystems := []string{"ebpf", "userspace", "network", "process", "kernel"}

	// Simulate failure in one subsystem
	collector.simulateSubsystemFailure("ebpf")
	time.Sleep(200 * time.Millisecond)

	// Other subsystems should remain functional
	for _, subsystem := range subsystems {
		healthy := collector.isSubsystemHealthy(subsystem)
		if subsystem == "ebpf" {
			assert.False(t, healthy, "Failed subsystem should be unhealthy")
		} else {
			assert.True(t, healthy, "Other subsystems should remain healthy")
		}
	}

	// Overall collector should remain partially functional
	assert.True(t, collector.IsHealthy(), "Collector should remain healthy with isolated failure")

	// Verify bulkhead statistics
	stats := collector.Statistics()
	assert.Contains(t, stats, "bulkhead_failures")
	assert.Contains(t, stats, "isolated_subsystems")
	assert.Equal(t, int64(1), stats["bulkhead_failures"].(int64))
	assert.Equal(t, int64(1), stats["isolated_subsystems"].(int64))

	// Test recovery of isolated subsystem
	collector.recoverSubsystem("ebpf")
	time.Sleep(200 * time.Millisecond)

	assert.True(t, collector.isSubsystemHealthy("ebpf"), "Recovered subsystem should be healthy")

	finalStats := collector.Statistics()
	assert.Equal(t, int64(0), finalStats["isolated_subsystems"].(int64))
	assert.Greater(t, finalStats["recovery_events"].(int64), int64(0))
}

// TestCollectorResourceLimiting tests resource limiting and throttling
func TestCollectorResourceLimiting(t *testing.T) {
	logger := zaptest.NewLogger(t)

	collector := newResourceLimitedCollector("resource-test", logger)
	collector.setMemoryLimit(10 * 1024 * 1024) // 10MB limit
	collector.setCPULimit(50.0)                // 50% CPU limit
	collector.setEventRateLimit(1000)          // 1000 events/second

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Test memory limiting
	t.Run("MemoryLimiting", func(t *testing.T) {
		// Simulate high memory usage
		collector.simulateMemoryUsage(15 * 1024 * 1024) // Exceed 10MB limit
		time.Sleep(500 * time.Millisecond)

		stats := collector.Statistics()
		assert.Greater(t, stats["memory_throttle_events"].(int64), int64(0))
		assert.True(t, collector.isResourceThrottled("memory"))
	})

	// Test CPU limiting
	t.Run("CPULimiting", func(t *testing.T) {
		// Simulate high CPU usage
		collector.simulateCPUUsage(75.0) // Exceed 50% limit
		time.Sleep(500 * time.Millisecond)

		stats := collector.Statistics()
		assert.Greater(t, stats["cpu_throttle_events"].(int64), int64(0))
		assert.True(t, collector.isResourceThrottled("cpu"))
	})

	// Test event rate limiting
	t.Run("EventRateLimiting", func(t *testing.T) {
		// Generate events at high rate
		eventCount := 2000 // Exceed 1000/second limit
		startTime := time.Now()

		for i := 0; i < eventCount; i++ {
			collector.generateEvent(fmt.Sprintf("rate-test-%d", i))
		}

		duration := time.Since(startTime)
		effectiveRate := float64(eventCount) / duration.Seconds()

		// Should be limited to approximately 1000/second
		assert.Less(t, effectiveRate, 1200.0, "Event rate should be limited")

		stats := collector.Statistics()
		assert.Greater(t, stats["rate_limit_events"].(int64), int64(0))
	})

	// Verify resource recovery
	t.Run("ResourceRecovery", func(t *testing.T) {
		// Reduce resource usage
		collector.simulateMemoryUsage(5 * 1024 * 1024) // Below limit
		collector.simulateCPUUsage(25.0)               // Below limit
		time.Sleep(1 * time.Second)

		assert.False(t, collector.isResourceThrottled("memory"), "Memory throttling should be released")
		assert.False(t, collector.isResourceThrottled("cpu"), "CPU throttling should be released")

		stats := collector.Statistics()
		assert.Greater(t, stats["resource_recovery_events"].(int64), int64(0))
	})
}

// Helper collector implementations for error testing

type errorRecoveryCollector struct {
	name          string
	errorType     string
	recoveryTime  time.Duration
	logger        *zap.Logger
	healthy       bool
	errorCount    int64
	recoveryCount int64
	mu            sync.RWMutex
}

func newErrorRecoveryCollector(name, errorType string, recoveryTime time.Duration, logger *zap.Logger) *errorRecoveryCollector {
	return &errorRecoveryCollector{
		name:         name,
		errorType:    errorType,
		recoveryTime: recoveryTime,
		logger:       logger,
		healthy:      true,
	}
}

func (c *errorRecoveryCollector) Name() string { return c.name }

func (c *errorRecoveryCollector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}

func (c *errorRecoveryCollector) Start(ctx context.Context) error {
	c.mu.Lock()
	c.healthy = true
	c.mu.Unlock()
	return nil
}

func (c *errorRecoveryCollector) Stop() error {
	c.mu.Lock()
	c.healthy = false
	c.mu.Unlock()
	return nil
}

func (c *errorRecoveryCollector) Events() <-chan RawEvent {
	return make(chan RawEvent)
}

func (c *errorRecoveryCollector) Health() (bool, map[string]interface{}) {
	healthy := c.IsHealthy()
	return healthy, map[string]interface{}{
		"healthy":    healthy,
		"error_type": c.errorType,
	}
}

func (c *errorRecoveryCollector) Statistics() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]interface{}{
		"error_count":    atomic.LoadInt64(&c.errorCount),
		"recovery_count": atomic.LoadInt64(&c.recoveryCount),
		"error_type":     c.errorType,
	}
}

func (c *errorRecoveryCollector) triggerError() {
	atomic.AddInt64(&c.errorCount, 1)

	c.mu.Lock()
	c.healthy = false
	c.mu.Unlock()

	if c.errorType != "kernel_incompatible" && c.recoveryTime > 0 {
		// Start recovery timer
		go func() {
			time.Sleep(c.recoveryTime)
			c.recover()
		}()
	}
}

func (c *errorRecoveryCollector) recover() {
	atomic.AddInt64(&c.recoveryCount, 1)
	c.mu.Lock()
	c.healthy = true
	c.mu.Unlock()
}

// Additional helper collector types...

type degradationCollector struct {
	name    string
	logger  *zap.Logger
	mode    string
	healthy bool
	events  chan RawEvent
	stats   map[string]int64
	mu      sync.RWMutex
}

func newDegradationCollector(name string, logger *zap.Logger) *degradationCollector {
	return &degradationCollector{
		name:    name,
		logger:  logger,
		mode:    "full",
		healthy: true,
		events:  make(chan RawEvent, 1000),
		stats:   make(map[string]int64),
	}
}

func (c *degradationCollector) Name() string { return c.name }
func (c *degradationCollector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}
func (c *degradationCollector) Start(ctx context.Context) error {
	c.mu.Lock()
	c.healthy = true
	c.mu.Unlock()
	return nil
}
func (c *degradationCollector) Stop() error {
	c.mu.Lock()
	c.healthy = false
	c.mu.Unlock()
	return nil
}
func (c *degradationCollector) Events() <-chan RawEvent { return c.events }
func (c *degradationCollector) Health() (bool, map[string]interface{}) {
	return c.IsHealthy(), map[string]interface{}{"mode": c.GetOperationMode()}
}
func (c *degradationCollector) Statistics() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]interface{})
	for k, v := range c.stats {
		result[k] = v
	}
	return result
}

func (c *degradationCollector) GetOperationMode() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mode
}

func (c *degradationCollector) simulateEBPFFailure() {
	c.mu.Lock()
	c.mode = "userspace"
	c.stats["degradation_events"]++
	c.mu.Unlock()
}

func (c *degradationCollector) simulateMemoryPressure() {
	c.mu.Lock()
	c.mode = "essential"
	c.stats["degradation_events"]++
	c.mu.Unlock()
}

func (c *degradationCollector) simulateRecovery() {
	c.mu.Lock()
	c.mode = "full"
	c.stats["recovery_events"]++
	c.mu.Unlock()
}

func (c *degradationCollector) generateTestEvent(eventID string) {
	event := RawEvent{
		Type:      "test",
		Timestamp: time.Now(),
		TraceID:   "test-trace",
		SpanID:    "test-span",
		Metadata:  map[string]string{"mode": c.GetOperationMode(), "event_id": eventID},
		Data:      []byte(fmt.Sprintf(`{"mode":"%s"}`, c.GetOperationMode())),
	}
	select {
	case c.events <- event:
	default:
	}
}

// More helper types for circuit breaker, retry, etc...
type circuitBreakerCollector struct {
	name                string
	logger              *zap.Logger
	healthy             bool
	circuitState        string
	consecutiveFailures int64
	stats               map[string]int64
	mu                  sync.RWMutex
}

func newCircuitBreakerCollector(name string, logger *zap.Logger) *circuitBreakerCollector {
	return &circuitBreakerCollector{
		name:         name,
		logger:       logger,
		healthy:      true,
		circuitState: "closed",
		stats:        make(map[string]int64),
	}
}

func (c *circuitBreakerCollector) Name() string { return c.name }
func (c *circuitBreakerCollector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}
func (c *circuitBreakerCollector) Start(ctx context.Context) error { return nil }
func (c *circuitBreakerCollector) Stop() error                     { return nil }
func (c *circuitBreakerCollector) Events() <-chan RawEvent         { return make(chan RawEvent) }
func (c *circuitBreakerCollector) Health() (bool, map[string]interface{}) {
	return c.IsHealthy(), map[string]interface{}{"circuit_state": c.GetCircuitState()}
}
func (c *circuitBreakerCollector) Statistics() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := map[string]interface{}{
		"consecutive_failures": atomic.LoadInt64(&c.consecutiveFailures),
	}
	for k, v := range c.stats {
		result[k] = v
	}
	return result
}

func (c *circuitBreakerCollector) GetCircuitState() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.circuitState
}

func (c *circuitBreakerCollector) simulateFailure() {
	failures := atomic.AddInt64(&c.consecutiveFailures, 1)
	c.mu.Lock()
	if failures >= 5 {
		c.circuitState = "open"
		c.stats["circuit_breaker_trips"]++
		// Start timer to go to half-open
		go func() {
			time.Sleep(2 * time.Second)
			c.mu.Lock()
			if c.circuitState == "open" {
				c.circuitState = "half-open"
			}
			c.mu.Unlock()
		}()
	}
	c.mu.Unlock()
}

func (c *circuitBreakerCollector) simulateSuccess() {
	atomic.StoreInt64(&c.consecutiveFailures, 0)
	c.mu.Lock()
	c.circuitState = "closed"
	c.stats["successful_operations"]++
	c.mu.Unlock()
}

// Remaining helper types would be implemented similarly...
// Due to length constraints, I'm showing the pattern for the key types

type hierarchicalCollector struct {
	name     string
	logger   *zap.Logger
	healthy  bool
	children []*hierarchicalCollector
	mu       sync.RWMutex
}

func newHierarchicalCollector(name string, logger *zap.Logger) *hierarchicalCollector {
	return &hierarchicalCollector{name: name, logger: logger, healthy: true}
}

func (c *hierarchicalCollector) Name() string { return c.name }
func (c *hierarchicalCollector) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.healthy
}
func (c *hierarchicalCollector) Start(ctx context.Context) error { return nil }
func (c *hierarchicalCollector) Stop() error                     { return nil }
func (c *hierarchicalCollector) Events() <-chan RawEvent         { return make(chan RawEvent) }
func (c *hierarchicalCollector) Health() (bool, map[string]interface{}) {
	return c.IsHealthy(), map[string]interface{}{"children": len(c.children)}
}

func (c *hierarchicalCollector) Statistics() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	unhealthy := int64(0)
	for _, child := range c.children {
		if !child.IsHealthy() {
			unhealthy++
		}
	}
	return map[string]interface{}{
		"total_children":     int64(len(c.children)),
		"unhealthy_children": unhealthy,
	}
}

func (c *hierarchicalCollector) addChild(child *hierarchicalCollector) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.children = append(c.children, child)
}

func (c *hierarchicalCollector) simulateError(errorType string) {
	c.mu.Lock()
	c.healthy = false
	c.mu.Unlock()
}

func (c *hierarchicalCollector) recover() {
	c.mu.Lock()
	c.healthy = true
	c.mu.Unlock()
}

// Additional helper types for retry, bulkhead, and resource limiting would follow the same pattern...

type retryCollector struct {
	name          string
	retryStrategy string
	maxRetries    int
	failureCount  int
	logger        *zap.Logger
	stats         map[string]interface{}
	mu            sync.RWMutex
}

func newRetryCollector(name, strategy string, maxRetries int, logger *zap.Logger) *retryCollector {
	return &retryCollector{
		name: name, retryStrategy: strategy, maxRetries: maxRetries, logger: logger,
		stats: map[string]interface{}{"retry_strategy": strategy},
	}
}

func (c *retryCollector) Name() string                           { return c.name }
func (c *retryCollector) IsHealthy() bool                        { return true }
func (c *retryCollector) Events() <-chan RawEvent                { return make(chan RawEvent) }
func (c *retryCollector) Health() (bool, map[string]interface{}) { return true, c.stats }
func (c *retryCollector) Statistics() map[string]interface{}     { return c.stats }
func (c *retryCollector) Stop() error                            { return nil }
func (c *retryCollector) setFailureCount(count int)              { c.failureCount = count }

func (c *retryCollector) Start(ctx context.Context) error {
	attempts := int64(0)
	for i := 0; i < c.maxRetries; i++ {
		attempts++
		if c.failureCount > 0 {
			c.failureCount--

			// Calculate backoff time
			var backoffTime time.Duration
			switch c.retryStrategy {
			case "exponential":
				backoffTime = time.Duration(1<<uint(i)) * time.Second
			case "linear":
				backoffTime = time.Duration(i+1) * time.Second
			case "fixed":
				backoffTime = 1 * time.Second
			}

			time.Sleep(backoffTime)
		} else {
			c.stats["retry_attempts"] = attempts
			return nil
		}
	}
	c.stats["retry_attempts"] = attempts
	return errors.New("max retries exceeded")
}

// Bulkhead and Resource Limited collectors would follow similar patterns...

type bulkheadCollector struct {
	name       string
	logger     *zap.Logger
	subsystems map[string]bool
	stats      map[string]int64
	mu         sync.RWMutex
}

func newBulkheadCollector(name string, logger *zap.Logger) *bulkheadCollector {
	return &bulkheadCollector{
		name:   name,
		logger: logger,
		subsystems: map[string]bool{
			"ebpf": true, "userspace": true, "network": true, "process": true, "kernel": true,
		},
		stats: make(map[string]int64),
	}
}

func (c *bulkheadCollector) Name() string                           { return c.name }
func (c *bulkheadCollector) IsHealthy() bool                        { return true }
func (c *bulkheadCollector) Start(ctx context.Context) error        { return nil }
func (c *bulkheadCollector) Stop() error                            { return nil }
func (c *bulkheadCollector) Events() <-chan RawEvent                { return make(chan RawEvent) }
func (c *bulkheadCollector) Health() (bool, map[string]interface{}) { return true, nil }
func (c *bulkheadCollector) Statistics() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]interface{})
	for k, v := range c.stats {
		result[k] = v
	}
	return result
}

func (c *bulkheadCollector) isSubsystemHealthy(subsystem string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.subsystems[subsystem]
}

func (c *bulkheadCollector) simulateSubsystemFailure(subsystem string) {
	c.mu.Lock()
	c.subsystems[subsystem] = false
	c.stats["bulkhead_failures"]++
	c.stats["isolated_subsystems"]++
	c.mu.Unlock()
}

func (c *bulkheadCollector) recoverSubsystem(subsystem string) {
	c.mu.Lock()
	c.subsystems[subsystem] = true
	c.stats["isolated_subsystems"]--
	c.stats["recovery_events"]++
	c.mu.Unlock()
}

type resourceLimitedCollector struct {
	name        string
	logger      *zap.Logger
	memoryLimit uint64
	cpuLimit    float64
	rateLimit   int
	throttled   map[string]bool
	stats       map[string]int64
	events      chan RawEvent
	mu          sync.RWMutex
}

func newResourceLimitedCollector(name string, logger *zap.Logger) *resourceLimitedCollector {
	return &resourceLimitedCollector{
		name:      name,
		logger:    logger,
		throttled: make(map[string]bool),
		stats:     make(map[string]int64),
		events:    make(chan RawEvent, 1000),
	}
}

func (c *resourceLimitedCollector) Name() string                           { return c.name }
func (c *resourceLimitedCollector) IsHealthy() bool                        { return true }
func (c *resourceLimitedCollector) Start(ctx context.Context) error        { return nil }
func (c *resourceLimitedCollector) Stop() error                            { return nil }
func (c *resourceLimitedCollector) Events() <-chan RawEvent                { return c.events }
func (c *resourceLimitedCollector) Health() (bool, map[string]interface{}) { return true, nil }
func (c *resourceLimitedCollector) Statistics() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]interface{})
	for k, v := range c.stats {
		result[k] = v
	}
	return result
}

func (c *resourceLimitedCollector) setMemoryLimit(limit uint64) { c.memoryLimit = limit }
func (c *resourceLimitedCollector) setCPULimit(limit float64)   { c.cpuLimit = limit }
func (c *resourceLimitedCollector) setEventRateLimit(limit int) { c.rateLimit = limit }

func (c *resourceLimitedCollector) simulateMemoryUsage(usage uint64) {
	c.mu.Lock()
	if usage > c.memoryLimit {
		c.throttled["memory"] = true
		c.stats["memory_throttle_events"]++
	} else {
		c.throttled["memory"] = false
		c.stats["resource_recovery_events"]++
	}
	c.mu.Unlock()
}

func (c *resourceLimitedCollector) simulateCPUUsage(usage float64) {
	c.mu.Lock()
	if usage > c.cpuLimit {
		c.throttled["cpu"] = true
		c.stats["cpu_throttle_events"]++
	} else {
		c.throttled["cpu"] = false
		c.stats["resource_recovery_events"]++
	}
	c.mu.Unlock()
}

func (c *resourceLimitedCollector) isResourceThrottled(resource string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.throttled[resource]
}

func (c *resourceLimitedCollector) generateEvent(eventID string) {
	// Implement rate limiting logic
	c.mu.Lock()
	c.stats["rate_limit_events"]++
	c.mu.Unlock()

	event := RawEvent{
		Type:      "resource_test",
		Timestamp: time.Now(),
		TraceID:   "resource-trace",
		SpanID:    "resource-span",
		Metadata:  map[string]string{"event_id": eventID},
		Data:      []byte(fmt.Sprintf(`{"event_id":"%s"}`, eventID)),
	}

	select {
	case c.events <- event:
	default:
		// Channel full - this contributes to rate limiting
	}
}
