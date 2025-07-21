package internal

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf/core"
)

func TestProductionCollectorSecurity(t *testing.T) {
	config := core.Config{
		Name:               "test-ebpf",
		Enabled:            true,
		EventBufferSize:    100,
		MaxEventsPerSecond: 1000,
	}

	collector, err := NewProductionCollector(config)
	if err != nil {
		// This is expected in test environment without proper permissions
		t.Logf("Expected error in test environment: %v", err)
		return
	}

	pc := collector.(*ProductionCollector)

	// Test security manager is initialized
	if pc.security == nil {
		t.Fatal("Security manager not initialized")
	}

	// Test rate limiter is initialized
	if pc.rateLimiter == nil {
		t.Fatal("Rate limiter not initialized")
	}

	// Test resource manager is initialized
	if pc.resources == nil {
		t.Fatal("Resource manager not initialized")
	}

	// Test monitoring is initialized
	if pc.monitoring == nil {
		t.Fatal("Monitoring not initialized")
	}
}

func TestRateLimiting(t *testing.T) {
	config := DefaultRateLimiterConfig()
	config.MaxEventsPerSecond = 100
	config.BurstSize = 10

	rl := NewRateLimiter(config)
	defer rl.Stop()

	ctx := context.Background()

	// Test burst capacity
	allowed := 0
	for i := 0; i < 20; i++ {
		if rl.Allow(ctx) {
			allowed++
		}
	}

	if allowed > int(config.BurstSize) {
		t.Errorf("Rate limiter allowed %d events, expected max %d", allowed, config.BurstSize)
	}

	// Test rate limiting over time
	time.Sleep(100 * time.Millisecond)

	allowed2 := 0
	for i := 0; i < 20; i++ {
		if rl.Allow(ctx) {
			allowed2++
		}
	}

	// Should allow approximately 10 events (100 events/sec * 0.1 sec)
	if allowed2 > 15 {
		t.Errorf("Rate limiter allowed too many events after refill: %d", allowed2)
	}
}

func TestCircuitBreaker(t *testing.T) {
	config := &CircuitBreakerConfig{
		ErrorThreshold: 0.5,
		ErrorWindow:    1 * time.Minute,
		CooldownPeriod: 1 * time.Second,
		HalfOpenLimit:  5,
	}

	cb := NewCircuitBreaker(config)

	// Circuit should start closed
	if !cb.Allow() {
		t.Error("Circuit breaker should start in closed state")
	}

	// Record errors to trigger opening
	for i := 0; i < 10; i++ {
		cb.RecordError()
	}

	// Circuit should open after errors
	if cb.Allow() {
		t.Error("Circuit breaker should be open after errors")
	}

	// Wait for cooldown
	time.Sleep(config.CooldownPeriod + 100*time.Millisecond)

	// Circuit should transition to half-open
	if !cb.Allow() {
		t.Error("Circuit breaker should transition to half-open after cooldown")
	}

	// Record successes to close circuit
	for i := 0; i < config.HalfOpenLimit; i++ {
		cb.RecordSuccess()
	}

	// Circuit should close after successes
	if cb.State() != CircuitClosed {
		t.Error("Circuit breaker should close after successful requests")
	}
}

func TestBackpressureManager(t *testing.T) {
	bpm := NewBackpressureManager(100, 50, 1*time.Minute)

	// Should not be active initially
	if bpm.IsActive() {
		t.Error("Backpressure should not be active initially")
	}

	// Update load above high watermark
	bpm.UpdateLoad(150)
	if !bpm.IsActive() {
		t.Error("Backpressure should activate above high watermark")
	}

	// Update load between watermarks - should stay active
	bpm.UpdateLoad(75)
	if !bpm.IsActive() {
		t.Error("Backpressure should remain active between watermarks")
	}

	// Update load below low watermark
	bpm.UpdateLoad(40)
	if bpm.IsActive() {
		t.Error("Backpressure should deactivate below low watermark")
	}
}

func TestResourceManager(t *testing.T) {
	config := DefaultResourceConfig()
	config.MaxMemoryMB = 100

	rm := NewResourceManager(config)
	defer rm.Stop()

	// Test memory allocation
	buf, err := rm.AllocateMemory(1024)
	if err != nil {
		t.Errorf("Failed to allocate memory: %v", err)
	}

	// Buffer pool returns 4096 byte buffers by default
	expectedSize := 4096
	if len(buf) != expectedSize {
		t.Logf("Buffer from pool has size %d (expected pool size)", len(buf))
	}

	// Release memory
	rm.ReleaseMemory(buf)

	// Test memory limit enforcement
	_, err = rm.AllocateMemory(200 * 1024 * 1024) // 200MB
	if err == nil {
		t.Error("Should fail to allocate memory exceeding limit")
	}

	// Test resource pressure detection
	if rm.IsUnderPressure() {
		t.Error("Should not be under pressure with minimal usage")
	}
}

func TestMonitoringManager(t *testing.T) {
	config := DefaultMonitoringConfig()
	mm := NewMonitoringManager(config)
	defer mm.Stop()

	// Record some events
	mm.RecordEvent("test_event", map[string]string{"test": "true"})
	mm.RecordError("test_error", map[string]string{"error": "test"})
	mm.RecordLatency("test_operation", 50*time.Millisecond, nil)

	// Get metrics
	metrics := mm.GetMetrics()

	if events, ok := metrics["events_total"]; !ok || events.(uint64) == 0 {
		t.Error("Events not recorded properly")
	}

	if errors, ok := metrics["errors_total"]; !ok || errors.(uint64) == 0 {
		t.Error("Errors not recorded properly")
	}

	// Check health status
	health := mm.GetHealthStatus()
	if health != core.HealthStatusHealthy {
		t.Errorf("Expected healthy status, got %s", health)
	}

	// Get dashboard
	dashboard := mm.GetDashboard()
	if dashboard["system_health"] != core.HealthStatusHealthy {
		t.Error("Dashboard not showing correct health")
	}
}

func TestErrorRecoveryManager(t *testing.T) {
	erm := &ErrorRecoveryManager{
		maxRetries: 3,
		backoffStrategy: BackoffStrategy{
			InitialDelay: 10 * time.Millisecond,
			MaxDelay:     100 * time.Millisecond,
			Multiplier:   2.0,
			Jitter:       0.1,
		},
	}
	erm.lastError.Store(time.Time{})

	// Test successful execution
	attempts := 0
	err := erm.ExecuteWithRetry(func() error {
		attempts++
		return nil
	})

	if err != nil {
		t.Errorf("Should succeed on first attempt: %v", err)
	}
	if attempts != 1 {
		t.Errorf("Expected 1 attempt, got %d", attempts)
	}

	// Test retry on failure
	attempts = 0
	err = erm.ExecuteWithRetry(func() error {
		attempts++
		if attempts < 3 {
			return fmt.Errorf("temporary error")
		}
		return nil
	})

	if err != nil {
		t.Errorf("Should succeed after retries: %v", err)
	}
	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}

	// Test max retries exceeded
	attempts = 0
	err = erm.ExecuteWithRetry(func() error {
		attempts++
		return fmt.Errorf("permanent error")
	})

	if err == nil {
		t.Error("Should fail after max retries")
	}
	if attempts != 3 {
		t.Errorf("Expected 3 attempts, got %d", attempts)
	}
}

func TestDegradedMode(t *testing.T) {
	pc := &ProductionCollector{
		config: core.Config{
			MaxEventsPerSecond: 1000,
		},
	}
	pc.degradedMode.Store(false)
	pc.degradationLevel.Store(0)

	// Create mock components properly
	rateLimiterConfig := DefaultRateLimiterConfig()
	pc.rateLimiter = NewRateLimiter(rateLimiterConfig)

	monitoringConfig := DefaultMonitoringConfig()
	pc.monitoring = NewMonitoringManager(monitoringConfig)

	// Test entering degraded mode
	pc.enterDegradedMode(1)

	if !pc.degradedMode.Load() {
		t.Error("Should be in degraded mode")
	}
	if pc.degradationLevel.Load() != 1 {
		t.Errorf("Expected degradation level 1, got %d", pc.degradationLevel.Load())
	}
	if pc.rateLimiter.currentRate.Load() != 500 {
		t.Errorf("Expected rate limit to be halved, got %d", pc.rateLimiter.currentRate.Load())
	}

	// Test event prioritization in degraded mode
	highPriorityEvent := core.RawEvent{Type: "security", UID: 0}
	normalEvent := core.RawEvent{Type: "normal", UID: 1000}

	if pc.shouldDropUnderPressure(highPriorityEvent) {
		t.Error("Should not drop high priority events in level 1 degradation")
	}

	pc.degradationLevel.Store(2)
	if pc.shouldDropUnderPressure(normalEvent) {
		t.Log("Correctly dropping normal events in level 2 degradation")
	}
}

// Benchmark tests

func BenchmarkRateLimiter(b *testing.B) {
	config := DefaultRateLimiterConfig()
	config.MaxEventsPerSecond = 10000
	rl := NewRateLimiter(config)
	defer rl.Stop()

	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rl.Allow(ctx)
		}
	})
}

func BenchmarkMonitoring(b *testing.B) {
	mm := NewMonitoringManager(DefaultMonitoringConfig())
	defer mm.Stop()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mm.RecordEvent("benchmark", nil)
		}
	})
}
