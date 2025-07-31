package adapters

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/hardening"
	"github.com/yairfalse/tapio/pkg/intelligence/interfaces"
)

// Mock correlation engine for testing
type mockProductionEngine struct {
	processedEvents []*domain.UnifiedEvent
	mu              sync.Mutex
	shouldFail      bool
	failCount       int
	started         bool
}

func (m *mockProductionEngine) Start() error {
	m.started = true
	return nil
}

func (m *mockProductionEngine) Stop() error {
	m.started = false
	return nil
}

func (m *mockProductionEngine) ProcessEvent(ctx context.Context, event *domain.UnifiedEvent) error {
	if m.shouldFail {
		m.failCount++
		return errors.New("processing failed")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.processedEvents = append(m.processedEvents, event)
	return nil
}

func (m *mockProductionEngine) GetLatestFindings() *interfaces.Finding {
	return nil
}

func (m *mockProductionEngine) GetSemanticGroups() []*interfaces.SemanticGroup {
	return nil
}

func (m *mockProductionEngine) GetProcessedEvents() []*domain.UnifiedEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*domain.UnifiedEvent, len(m.processedEvents))
	copy(result, m.processedEvents)
	return result
}

func TestProductionHardeningAdapter_NewAdapter(t *testing.T) {
	engine := &mockProductionEngine{}
	adapter := NewProductionHardeningAdapter(engine)

	if adapter.engine != engine {
		t.Error("Engine not set correctly")
	}

	if adapter.rateLimiter == nil {
		t.Error("Rate limiter not initialized")
	}

	if adapter.circuitBreaker == nil {
		t.Error("Circuit breaker not initialized")
	}

	if adapter.backpressure == nil {
		t.Error("Backpressure controller not initialized")
	}

	if adapter.resourceMonitor == nil {
		t.Error("Resource monitor not initialized")
	}
}

func TestProductionHardeningAdapter_WithOptions(t *testing.T) {
	engine := &mockProductionEngine{}
	adapter := NewProductionHardeningAdapter(engine,
		WithRateLimit(5000),
		WithMemoryLimit(512*1024*1024), // 512MB
	)

	// Check rate limiter was configured
	metrics := adapter.rateLimiter.GetMetrics()
	if metrics.CurrentTokens > 5000 {
		t.Error("Rate limit not applied correctly")
	}

	// Check memory limit
	if adapter.resourceMonitor.maxMemoryBytes != 512*1024*1024 {
		t.Error("Memory limit not set correctly")
	}
}

func TestProductionHardeningAdapter_RateLimiting(t *testing.T) {
	engine := &mockProductionEngine{}
	adapter := NewProductionHardeningAdapter(engine,
		WithRateLimit(2), // Only 2 events per second
	)

	ctx := context.Background()
	event := &domain.UnifiedEvent{
		ID:   "test-1",
		Type: domain.EventTypeSystem,
	}

	// First two should succeed
	err := adapter.ProcessEvent(ctx, event)
	if err != nil {
		t.Errorf("First event should succeed: %v", err)
	}

	err = adapter.ProcessEvent(ctx, event)
	if err != nil {
		t.Errorf("Second event should succeed: %v", err)
	}

	// Third should be rate limited
	err = adapter.ProcessEvent(ctx, event)
	if err != ErrRateLimitExceeded {
		t.Errorf("Expected rate limit error, got: %v", err)
	}
}

func TestProductionHardeningAdapter_CircuitBreaker(t *testing.T) {
	engine := &mockProductionEngine{shouldFail: true}
	adapter := NewProductionHardeningAdapter(engine)

	// Need to configure circuit breaker with low threshold for testing
	adapter.circuitBreaker = hardening.NewCircuitBreaker(2, time.Hour)

	ctx := context.Background()
	event := &domain.UnifiedEvent{
		ID:   "test-1",
		Type: domain.EventTypeSystem,
	}

	// Cause failures to open circuit
	adapter.ProcessEvent(ctx, event)
	adapter.ProcessEvent(ctx, event)

	// Next call should be rejected by circuit breaker
	err := adapter.ProcessEvent(ctx, event)
	if err != ErrCircuitBreakerOpen {
		t.Errorf("Expected circuit breaker open error, got: %v", err)
	}
}

func TestProductionHardeningAdapter_Backpressure(t *testing.T) {
	engine := &mockProductionEngine{}
	adapter := NewProductionHardeningAdapter(engine)

	// Set high load to trigger backpressure
	adapter.backpressure.UpdateLoad(9000) // 90% of 10000 buffer

	ctx := context.Background()

	// Critical event should always pass
	criticalEvent := &domain.UnifiedEvent{
		ID:       "critical-1",
		Type:     domain.EventTypeSystem,
		Severity: domain.EventSeverityCritical,
	}
	err := adapter.ProcessEvent(ctx, criticalEvent)
	if err != nil {
		t.Errorf("Critical event should pass even under high load: %v", err)
	}

	// Low priority event might be shed
	lowPriorityEvent := &domain.UnifiedEvent{
		ID:       "low-1",
		Type:     domain.EventTypeLog,
		Severity: domain.EventSeverityInfo,
	}

	// With 90% load, low priority events have high chance of being shed
	// Run multiple times to account for randomness
	shedCount := 0
	for i := 0; i < 10; i++ {
		err = adapter.ProcessEvent(ctx, lowPriorityEvent)
		if err == ErrBackpressure {
			shedCount++
		}
	}

	if shedCount == 0 {
		t.Error("Expected some low priority events to be shed under high load")
	}
}

func TestProductionHardeningAdapter_ProcessBatch(t *testing.T) {
	engine := &mockProductionEngine{}
	adapter := NewProductionHardeningAdapter(engine,
		WithRateLimit(10),
	)

	ctx := context.Background()
	events := []*domain.UnifiedEvent{
		{ID: "1", Type: domain.EventTypeSystem},
		{ID: "2", Type: domain.EventTypeSystem},
		{ID: "3", Type: domain.EventTypeSystem},
		{ID: "4", Type: domain.EventTypeSystem},
		{ID: "5", Type: domain.EventTypeSystem},
	}

	err := adapter.ProcessBatch(ctx, events)
	if err != nil {
		t.Errorf("Batch processing failed: %v", err)
	}

	// Try to exceed rate limit with large batch
	largeEvents := make([]*domain.UnifiedEvent, 20)
	for i := range largeEvents {
		largeEvents[i] = &domain.UnifiedEvent{
			ID:   string(rune(i)),
			Type: domain.EventTypeSystem,
		}
	}

	err = adapter.ProcessBatch(ctx, largeEvents)
	if err != ErrRateLimitExceeded {
		t.Errorf("Expected rate limit error for large batch, got: %v", err)
	}
}

func TestProductionHardeningAdapter_StartStop(t *testing.T) {
	engine := &mockProductionEngine{}
	adapter := NewProductionHardeningAdapter(engine)

	// Start adapter
	err := adapter.Start()
	if err != nil {
		t.Errorf("Failed to start adapter: %v", err)
	}

	if !engine.started {
		t.Error("Engine should be started")
	}

	// Let monitoring loops run briefly
	time.Sleep(100 * time.Millisecond)

	// Stop adapter
	err = adapter.Stop()
	if err != nil {
		t.Errorf("Failed to stop adapter: %v", err)
	}

	if engine.started {
		t.Error("Engine should be stopped")
	}
}

func TestProductionHardeningAdapter_GetMetrics(t *testing.T) {
	engine := &mockProductionEngine{}
	adapter := NewProductionHardeningAdapter(engine)

	err := adapter.Start()
	if err != nil {
		t.Fatalf("Failed to start adapter: %v", err)
	}
	defer adapter.Stop()

	ctx := context.Background()

	// Process some events
	for i := 0; i < 5; i++ {
		event := &domain.UnifiedEvent{
			ID:   string(rune(i)),
			Type: domain.EventTypeSystem,
		}
		adapter.ProcessEvent(ctx, event)
	}

	// Wait for metrics update
	time.Sleep(1100 * time.Millisecond)

	metrics := adapter.GetMetrics()
	if metrics == nil {
		t.Error("GetMetrics should return non-nil metrics")
	}

	// Should have rate utilization data
	if metrics.RateUtilization < 0 {
		t.Error("Rate utilization should be non-negative")
	}

	// Should have circuit breaker state
	if metrics.CircuitState == "" {
		t.Error("Circuit state should not be empty")
	}

	// Should have load level
	if metrics.LoadLevel == "" {
		t.Error("Load level should not be empty")
	}
}

func TestBackpressureController_Accept(t *testing.T) {
	bc := &BackpressureController{
		bufferSize:        1000,
		highWatermark:     0.7,
		criticalWatermark: 0.9,
	}
	bc.shedRate.Store(float64(0))

	// Test normal load (50%)
	bc.UpdateLoad(500)

	event := &domain.UnifiedEvent{
		ID:       "test-1",
		Type:     domain.EventTypeSystem,
		Severity: domain.EventSeverityInfo,
	}

	if !bc.Accept(event) {
		t.Error("Should accept events under normal load")
	}

	// Test high load (80%)
	bc.UpdateLoad(800)

	// High priority should still be accepted
	highPriorityEvent := &domain.UnifiedEvent{
		ID:       "test-2",
		Type:     domain.EventTypeSystem,
		Severity: domain.EventSeverityError,
	}

	if !bc.Accept(highPriorityEvent) {
		t.Error("Should accept high priority events under high load")
	}

	// Test critical load (95%)
	bc.UpdateLoad(950)

	// Only critical events should pass
	lowPriorityEvent := &domain.UnifiedEvent{
		ID:       "test-3",
		Type:     domain.EventTypeLog,
		Severity: domain.EventSeverityInfo,
	}

	if bc.Accept(lowPriorityEvent) {
		t.Error("Should reject low priority events under critical load")
	}

	criticalEvent := &domain.UnifiedEvent{
		ID:       "test-4",
		Type:     domain.EventTypeSystem,
		Severity: domain.EventSeverityCritical,
	}

	if !bc.Accept(criticalEvent) {
		t.Error("Should always accept critical events")
	}
}

func TestBackpressureController_FilterBatch(t *testing.T) {
	bc := &BackpressureController{
		bufferSize:        1000,
		highWatermark:     0.7,
		criticalWatermark: 0.9,
	}
	bc.shedRate.Store(float64(0))

	events := []*domain.UnifiedEvent{
		{ID: "1", Severity: domain.EventSeverityCritical},
		{ID: "2", Severity: domain.EventSeverityError},
		{ID: "3", Severity: domain.EventSeverityWarning},
		{ID: "4", Severity: domain.EventSeverityInfo},
		{ID: "5", Severity: domain.EventSeverityInfo},
	}

	// Normal load - all events should pass
	bc.UpdateLoad(500)
	filtered := bc.FilterBatch(events)
	if len(filtered) != len(events) {
		t.Errorf("Expected all events to pass under normal load, got %d/%d", len(filtered), len(events))
	}

	// Critical load - only high priority should pass
	bc.UpdateLoad(950)
	filtered = bc.FilterBatch(events)

	// Should include critical and error events (first 2)
	if len(filtered) < 2 {
		t.Error("Should include at least critical and error events")
	}

	// Check that critical event is included
	found := false
	for _, e := range filtered {
		if e.ID == "1" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Critical event should always be included")
	}
}

func TestResourceMonitor_CheckLimits(t *testing.T) {
	violationCalled := false
	rm := &ResourceMonitor{
		maxMemoryBytes: 1, // Impossibly low to trigger violation
		maxGoroutines:  1, // Impossibly low
		checkInterval:  time.Second,
		violationHandler: func(violation string) {
			violationCalled = true
		},
	}

	// This should trigger memory violation
	err := rm.CheckLimits()
	if err != ErrResourceExhausted {
		t.Errorf("Expected resource exhausted error, got: %v", err)
	}

	if !violationCalled {
		t.Error("Violation handler should have been called")
	}
}

func TestProductionHardeningAdapter_ConcurrentAccess(t *testing.T) {
	engine := &mockProductionEngine{}
	adapter := NewProductionHardeningAdapter(engine,
		WithRateLimit(10000), // High limit to avoid rate limiting
	)

	err := adapter.Start()
	if err != nil {
		t.Fatalf("Failed to start adapter: %v", err)
	}
	defer adapter.Stop()

	ctx := context.Background()
	const numGoroutines = 10
	const eventsPerGoroutine = 100

	var wg sync.WaitGroup
	errors := make([]error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				event := &domain.UnifiedEvent{
					ID:   string(rune(id*eventsPerGoroutine + j)),
					Type: domain.EventTypeSystem,
				}
				if err := adapter.ProcessEvent(ctx, event); err != nil {
					errors[id] = err
					return
				}
			}
		}(i)
	}

	wg.Wait()

	// Check for errors
	for i, err := range errors {
		if err != nil {
			t.Errorf("Goroutine %d encountered error: %v", i, err)
		}
	}

	// Should have processed many events
	processedCount := len(engine.GetProcessedEvents())
	if processedCount == 0 {
		t.Error("No events were processed")
	}
}

func BenchmarkProductionHardeningAdapter_ProcessEvent(b *testing.B) {
	engine := &mockProductionEngine{}
	adapter := NewProductionHardeningAdapter(engine,
		WithRateLimit(1000000), // Very high to not affect benchmark
	)

	ctx := context.Background()
	event := &domain.UnifiedEvent{
		ID:   "bench-1",
		Type: domain.EventTypeSystem,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			adapter.ProcessEvent(ctx, event)
		}
	})
}

func BenchmarkBackpressureController_Accept(b *testing.B) {
	bc := &BackpressureController{
		bufferSize:        10000,
		highWatermark:     0.7,
		criticalWatermark: 0.9,
	}
	bc.shedRate.Store(float64(0))
	bc.UpdateLoad(5000) // 50% load

	event := &domain.UnifiedEvent{
		ID:       "bench",
		Type:     domain.EventTypeSystem,
		Severity: domain.EventSeverityInfo,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bc.Accept(event)
	}
}
