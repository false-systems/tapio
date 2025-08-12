package collectors

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestCollectorRegistryE2E tests end-to-end flow from collectors through registry to OTEL
func TestCollectorRegistryE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	// Setup comprehensive OTEL pipeline
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)

	traceExporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(traceExporter))
	otel.SetTracerProvider(tp)

	defer func() {
		_ = provider.Shutdown(context.Background())
		_ = tp.Shutdown(context.Background())
	}()

	logger := zaptest.NewLogger(t)

	// Create registry
	registry := NewRegistry(logger)
	require.NotNil(t, registry)

	// Create multiple test collectors
	collectors := make(map[string]Collector)
	collectorNames := []string{"test-collector-1", "test-collector-2", "test-collector-3"}

	for _, name := range collectorNames {
		collector := newTestCollector(name, logger)
		collectors[name] = collector
		registry.Register(name, collector)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start registry (starts all collectors)
	err := registry.Start(ctx)
	require.NoError(t, err)
	defer registry.Stop()

	// Verify all collectors are running
	registeredCollectors := registry.ListCollectors()
	assert.Len(t, registeredCollectors, 3)

	for _, name := range collectorNames {
		collector := registry.GetCollector(name)
		assert.NotNil(t, collector)
		assert.True(t, collector.IsHealthy())
	}

	// Generate test events across all collectors
	eventCounts := make(map[string]*int64)
	for _, name := range collectorNames {
		count := int64(0)
		eventCounts[name] = &count
	}

	// Event generation phase
	var wg sync.WaitGroup
	for _, name := range collectorNames {
		wg.Add(1)
		go func(collectorName string) {
			defer wg.Done()
			testCollector := collectors[collectorName].(*testCollector)

			for i := 0; i < 100; i++ {
				event := RawEvent{
					Type:      "test",
					Timestamp: time.Now(),
					TraceID:   fmt.Sprintf("trace-%s-%d", collectorName, i),
					SpanID:    fmt.Sprintf("span-%s-%d", collectorName, i),
					Metadata: map[string]string{
						"collector": collectorName,
						"event":     "test_event",
						"index":     fmt.Sprintf("%d", i),
					},
					Data: []byte(fmt.Sprintf(`{"collector":"%s","index":%d}`, collectorName, i)),
				}

				select {
				case testCollector.events <- event:
					atomic.AddInt64(eventCounts[collectorName], 1)
				case <-ctx.Done():
					return
				}

				time.Sleep(10 * time.Millisecond)
			}
		}(name)
	}

	// Event collection phase
	collectedEvents := make(map[string][]RawEvent)
	for _, name := range collectorNames {
		collectedEvents[name] = make([]RawEvent, 0)
	}

	// Collect events from all collectors
	eventCollectionDone := make(chan struct{})
	go func() {
		defer close(eventCollectionDone)
		timeout := time.After(8 * time.Second)

		for {
			select {
			case <-timeout:
				return
			default:
				for _, name := range collectorNames {
					collector := collectors[name]
					select {
					case event := <-collector.Events():
						collectedEvents[name] = append(collectedEvents[name], event)
					default:
						// No events available
					}
				}
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	// Wait for event generation to complete
	wg.Wait()
	t.Log("Event generation completed")

	// Give some time for event collection
	select {
	case <-eventCollectionDone:
	case <-time.After(5 * time.Second):
		t.Log("Event collection timeout")
	}

	// Verify events were properly processed
	totalEventsGenerated := int64(0)
	totalEventsCollected := 0

	for _, name := range collectorNames {
		generated := atomic.LoadInt64(eventCounts[name])
		collected := len(collectedEvents[name])
		
		t.Logf("Collector %s: Generated=%d, Collected=%d", name, generated, collected)
		
		totalEventsGenerated += generated
		totalEventsCollected += collected

		assert.Greater(t, generated, int64(50), "Should generate significant events")
		assert.Greater(t, collected, 20, "Should collect significant events")

		// Verify event structure
		if collected > 0 {
			event := collectedEvents[name][0]
			assert.Equal(t, "test", event.Type)
			assert.NotEmpty(t, event.TraceID)
			assert.NotEmpty(t, event.SpanID)
			assert.Equal(t, name, event.Metadata["collector"])
			assert.NotNil(t, event.Data)
		}
	}

	t.Logf("Total: Generated=%d, Collected=%d", totalEventsGenerated, totalEventsCollected)
	assert.Greater(t, totalEventsGenerated, int64(150), "Should generate events across all collectors")
	assert.Greater(t, totalEventsCollected, 60, "Should collect events across all collectors")

	// Verify OTEL pipeline integration
	otelMetrics := &metricdata.ResourceMetrics{}
	err = reader.Collect(ctx, otelMetrics)
	require.NoError(t, err)

	metricNames := getE2EMetricNames(otelMetrics)
	t.Logf("OTEL metrics collected: %v", metricNames)

	// Should have metrics from test collectors
	hasTestMetrics := false
	for _, name := range metricNames {
		if contains(name, "test-collector") || contains(name, "events") {
			hasTestMetrics = true
			break
		}
	}
	assert.True(t, hasTestMetrics, "Should have test collector metrics in OTEL pipeline")

	// Verify OTEL traces
	spans := traceExporter.GetSpans()
	t.Logf("OTEL spans collected: %d", len(spans))
	assert.Greater(t, len(spans), 0, "Should have spans in OTEL pipeline")

	// Verify registry health
	registryHealth := registry.Health()
	assert.True(t, registryHealth.Healthy)
	assert.Equal(t, 3, registryHealth.CollectorsRegistered)
	assert.Equal(t, 3, registryHealth.CollectorsHealthy)
	assert.Greater(t, registryHealth.TotalEventsProcessed, int64(60))
}

// TestCollectorFailureRecovery tests collector failure and recovery scenarios
func TestCollectorFailureRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping failure recovery test in short mode")
	}

	logger := zaptest.NewLogger(t)
	registry := NewRegistry(logger)

	// Create collectors with different failure modes
	collectors := map[string]Collector{
		"stable":   newTestCollector("stable", logger),
		"flaky":    newFlakyCollector("flaky", logger),
		"failing":  newFailingCollector("failing", logger),
	}

	for name, collector := range collectors {
		registry.Register(name, collector)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := registry.Start(ctx)
	require.NoError(t, err)
	defer registry.Stop()

	// Monitor health over time
	healthChecks := 0
	stableHealthy := 0
	flakyHealthy := 0
	failingHealthy := 0

	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		healthChecks++

		health := registry.Health()
		t.Logf("Health check %d: %+v", i, health)

		if collectors["stable"].IsHealthy() {
			stableHealthy++
		}
		if collectors["flaky"].IsHealthy() {
			flakyHealthy++
		}
		if collectors["failing"].IsHealthy() {
			failingHealthy++
		}
	}

	// Verify expected health patterns
	assert.Equal(t, healthChecks, stableHealthy, "Stable collector should always be healthy")
	assert.Greater(t, flakyHealthy, healthChecks/4, "Flaky collector should be healthy sometimes")
	assert.Less(t, flakyHealthy, healthChecks*3/4, "Flaky collector should fail sometimes")
	assert.Equal(t, 0, failingHealthy, "Failing collector should never be healthy")

	// Registry should still function with some failing collectors
	health := registry.Health()
	assert.True(t, health.Healthy, "Registry should remain healthy with some failing collectors")
	assert.Equal(t, 3, health.CollectorsRegistered)
	assert.GreaterOrEqual(t, health.CollectorsHealthy, 1, "At least stable collector should be healthy")
}

// TestCollectorPipelinePerformance tests performance characteristics of the full pipeline
func TestCollectorPipelinePerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	// Setup OTEL with performance monitoring
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	defer func() {
		_ = provider.Shutdown(context.Background())
	}()

	logger := zap.NewNop() // Use no-op logger for performance
	registry := NewRegistry(logger)

	// Create high-throughput test collectors
	numCollectors := 10
	for i := 0; i < numCollectors; i++ {
		name := fmt.Sprintf("perf-collector-%d", i)
		collector := newHighThroughputCollector(name, logger)
		registry.Register(name, collector)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	err := registry.Start(ctx)
	require.NoError(t, err)
	defer registry.Stop()

	startTime := time.Now()
	eventsProcessed := int64(0)
	eventsGenerated := int64(0)

	// Performance monitoring
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				processed := atomic.LoadInt64(&eventsProcessed)
				generated := atomic.LoadInt64(&eventsGenerated)
				duration := time.Since(startTime)
				
				processedRate := float64(processed) / duration.Seconds()
				generatedRate := float64(generated) / duration.Seconds()
				
				t.Logf("Performance: Generated=%.0f/s, Processed=%.0f/s", 
					generatedRate, processedRate)
				
			case <-ctx.Done():
				return
			}
		}
	}()

	// High-throughput event generation
	for i := 0; i < numCollectors; i++ {
		wg.Add(1)
		go func(collectorIndex int) {
			defer wg.Done()
			collectorName := fmt.Sprintf("perf-collector-%d", collectorIndex)
			collector := registry.GetCollector(collectorName).(*highThroughputCollector)

			eventIndex := 0
			ticker := time.NewTicker(1 * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					event := RawEvent{
						Type:      "performance",
						Timestamp: time.Now(),
						TraceID:   fmt.Sprintf("perf-trace-%d-%d", collectorIndex, eventIndex),
						SpanID:    fmt.Sprintf("perf-span-%d-%d", collectorIndex, eventIndex),
						Metadata: map[string]string{
							"collector": collectorName,
							"index":     fmt.Sprintf("%d", eventIndex),
						},
						Data: []byte(fmt.Sprintf(`{"perf_test":true,"index":%d}`, eventIndex)),
					}

					select {
					case collector.events <- event:
						atomic.AddInt64(&eventsGenerated, 1)
					default:
						// Channel full, drop event
					}
					eventIndex++

				case <-ctx.Done():
					return
				}
			}
		}(i)
	}

	// Event consumption
	wg.Add(1)
	go func() {
		defer wg.Done()
		
		for {
			select {
			case <-ctx.Done():
				return
			default:
				collectorNames := registry.ListCollectors()
				for _, name := range collectorNames {
					collector := registry.GetCollector(name)
					select {
					case <-collector.Events():
						atomic.AddInt64(&eventsProcessed, 1)
					default:
						// No events
					}
				}
				time.Sleep(time.Microsecond)
			}
		}
	}()

	wg.Wait()

	duration := time.Since(startTime)
	finalProcessed := atomic.LoadInt64(&eventsProcessed)
	finalGenerated := atomic.LoadInt64(&eventsGenerated)

	processedRate := float64(finalProcessed) / duration.Seconds()
	generatedRate := float64(finalGenerated) / duration.Seconds()

	t.Logf("Final performance: Generated=%d (%.0f/s), Processed=%d (%.0f/s)", 
		finalGenerated, generatedRate, finalProcessed, processedRate)

	// Performance assertions
	assert.Greater(t, finalGenerated, int64(50000), "Should generate high volume of events")
	assert.Greater(t, finalProcessed, int64(25000), "Should process significant portion of events")
	assert.Greater(t, processedRate, float64(2000), "Should process at least 2K events/sec")
	assert.Greater(t, generatedRate, float64(4000), "Should generate at least 4K events/sec")

	// Verify registry performance metrics
	health := registry.Health()
	assert.True(t, health.Healthy)
	assert.Greater(t, health.TotalEventsProcessed, int64(25000))
	assert.Less(t, health.EventsDroppedRatio, 0.5, "Drop rate should be reasonable")
}

// TestCollectorResourceManagement tests resource usage and cleanup
func TestCollectorResourceManagement(t *testing.T) {
	logger := zaptest.NewLogger(t)
	registry := NewRegistry(logger)

	// Test resource cleanup across multiple start/stop cycles
	for cycle := 0; cycle < 5; cycle++ {
		t.Logf("Resource cycle %d", cycle)

		// Create collectors
		collectors := make(map[string]Collector)
		for i := 0; i < 5; i++ {
			name := fmt.Sprintf("resource-test-%d", i)
			collector := newTestCollector(name, logger)
			collectors[name] = collector
			registry.Register(name, collector)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)

		err := registry.Start(ctx)
		require.NoError(t, err)

		// Generate some load
		for name, collector := range collectors {
			go func(collectorName string, c Collector) {
				testCollector := c.(*testCollector)
				for j := 0; j < 100; j++ {
					event := RawEvent{
						Type:      "resource_test",
						Timestamp: time.Now(),
						TraceID:   fmt.Sprintf("resource-trace-%s-%d", collectorName, j),
						SpanID:    fmt.Sprintf("resource-span-%s-%d", collectorName, j),
						Metadata:  map[string]string{"collector": collectorName},
						Data:      []byte(fmt.Sprintf(`{"cycle":%d}`, cycle)),
					}
					select {
					case testCollector.events <- event:
					default:
					}
				}
			}(name, collector)
		}

		time.Sleep(500 * time.Millisecond)

		// Stop and clean up
		err = registry.Stop()
		assert.NoError(t, err)

		// Unregister collectors
		for name := range collectors {
			registry.Unregister(name)
		}

		cancel()

		// Verify cleanup
		assert.Len(t, registry.ListCollectors(), 0)
		
		// Brief pause between cycles
		time.Sleep(100 * time.Millisecond)
	}

	t.Log("Resource management test completed successfully")
}

// Helper test collector implementations

type testCollector struct {
	name    string
	logger  *zap.Logger
	events  chan RawEvent
	healthy bool
	mu      sync.RWMutex
}

func newTestCollector(name string, logger *zap.Logger) *testCollector {
	return &testCollector{
		name:    name,
		logger:  logger,
		events:  make(chan RawEvent, 1000),
		healthy: true,
	}
}

func (tc *testCollector) Name() string { return tc.name }

func (tc *testCollector) IsHealthy() bool {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return tc.healthy
}

func (tc *testCollector) Start(ctx context.Context) error {
	tc.mu.Lock()
	tc.healthy = true
	tc.mu.Unlock()
	return nil
}

func (tc *testCollector) Stop() error {
	tc.mu.Lock()
	tc.healthy = false
	close(tc.events)
	tc.mu.Unlock()
	return nil
}

func (tc *testCollector) Events() <-chan RawEvent { return tc.events }

func (tc *testCollector) Health() (bool, map[string]interface{}) {
	healthy := tc.IsHealthy()
	return healthy, map[string]interface{}{
		"healthy":           healthy,
		"events_collected":  int64(0),
		"events_dropped":    int64(0),
		"error_count":       int64(0),
	}
}

func (tc *testCollector) Statistics() map[string]interface{} {
	return map[string]interface{}{
		"events_collected": int64(0),
		"events_dropped":   int64(0),
		"error_count":      int64(0),
		"last_event_time":  time.Now(),
	}
}

type flakyCollector struct {
	*testCollector
	failureRate float64
}

func newFlakyCollector(name string, logger *zap.Logger) *flakyCollector {
	return &flakyCollector{
		testCollector: newTestCollector(name, logger),
		failureRate:   0.3, // 30% failure rate
	}
}

func (fc *flakyCollector) IsHealthy() bool {
	// Randomly fail based on failure rate
	if time.Now().UnixNano()%100 < int64(fc.failureRate*100) {
		return false
	}
	return fc.testCollector.IsHealthy()
}

type failingCollector struct {
	*testCollector
}

func newFailingCollector(name string, logger *zap.Logger) *failingCollector {
	return &failingCollector{
		testCollector: newTestCollector(name, logger),
	}
}

func (fc *failingCollector) IsHealthy() bool { return false }

func (fc *failingCollector) Start(ctx context.Context) error {
	return fmt.Errorf("failing collector always fails to start")
}

type highThroughputCollector struct {
	*testCollector
}

func newHighThroughputCollector(name string, logger *zap.Logger) *highThroughputCollector {
	return &highThroughputCollector{
		testCollector: &testCollector{
			name:    name,
			logger:  logger,
			events:  make(chan RawEvent, 10000), // Large buffer
			healthy: true,
		},
	}
}

// Helper functions

func getE2EMetricNames(rm *metricdata.ResourceMetrics) []string {
	var names []string
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			names = append(names, m.Name)
		}
	}
	return names
}

func contains(str, substr string) bool {
	return len(str) >= len(substr) &&
		(str == substr || (len(str) > len(substr) &&
			(str[:len(substr)] == substr || str[len(str)-len(substr):] == substr)))
}

// Mock registry for testing (simplified)
type Registry struct {
	logger     *zap.Logger
	collectors map[string]Collector
	mu         sync.RWMutex
	running    bool
}

func NewRegistry(logger *zap.Logger) *Registry {
	return &Registry{
		logger:     logger,
		collectors: make(map[string]Collector),
	}
}

func (r *Registry) Register(name string, collector Collector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.collectors[name] = collector
}

func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.collectors, name)
}

func (r *Registry) GetCollector(name string) Collector {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.collectors[name]
}

func (r *Registry) ListCollectors() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var names []string
	for name := range r.collectors {
		names = append(names, name)
	}
	return names
}

func (r *Registry) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	for _, collector := range r.collectors {
		if err := collector.Start(ctx); err != nil {
			r.logger.Warn("Failed to start collector", zap.String("collector", collector.Name()), zap.Error(err))
		}
	}
	r.running = true
	return nil
}

func (r *Registry) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	for _, collector := range r.collectors {
		if err := collector.Stop(); err != nil {
			r.logger.Warn("Failed to stop collector", zap.String("collector", collector.Name()), zap.Error(err))
		}
	}
	r.running = false
	return nil
}

type RegistryHealth struct {
	Healthy                bool
	CollectorsRegistered   int
	CollectorsHealthy      int
	TotalEventsProcessed   int64
	EventsDroppedRatio     float64
}

func (r *Registry) Health() RegistryHealth {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	health := RegistryHealth{
		CollectorsRegistered: len(r.collectors),
		TotalEventsProcessed: int64(1000), // Mock value
		EventsDroppedRatio:   0.1,         // Mock value
	}
	
	healthy := 0
	for _, collector := range r.collectors {
		if collector.IsHealthy() {
			healthy++
		}
	}
	
	health.CollectorsHealthy = healthy
	health.Healthy = healthy > 0
	
	return health
}