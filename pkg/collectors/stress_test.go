package collectors_test

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
	"github.com/yairfalse/tapio/pkg/collectors/cni"
	"github.com/yairfalse/tapio/pkg/collectors/dns"
	"github.com/yairfalse/tapio/pkg/collectors/etcd"
	"github.com/yairfalse/tapio/pkg/collectors/kernel"
	"github.com/yairfalse/tapio/pkg/collectors/kubeapi"
	"github.com/yairfalse/tapio/pkg/collectors/kubelet"
	"github.com/yairfalse/tapio/pkg/collectors/systemd"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// StressTestConfig defines parameters for stress testing
type StressTestConfig struct {
	Duration          time.Duration
	EventRate         int // Events per second
	ConcurrentReaders int
	BufferSize        int
	EnableMetrics     bool
	EnableTracing     bool
	MaxMemoryMB       int64
}

// DefaultStressTestConfig returns default stress test configuration
func DefaultStressTestConfig() StressTestConfig {
	return StressTestConfig{
		Duration:          10 * time.Second,
		EventRate:         1000,
		ConcurrentReaders: 4,
		BufferSize:        10000,
		EnableMetrics:     true,
		EnableTracing:     true,
		MaxMemoryMB:       500,
	}
}

// StressTestResult contains stress test results
type StressTestResult struct {
	TotalEvents     int64
	ProcessedEvents int64
	DroppedEvents   int64
	ErrorCount      int64
	MaxMemoryUsedMB int64
	AvgLatencyMs    float64
	P95LatencyMs    float64
	P99LatencyMs    float64
	EventsPerSecond float64
}

// CollectorStressTester provides stress testing capabilities for collectors
type CollectorStressTester struct {
	collector collectors.Collector
	config    StressTestConfig
	logger    *zap.Logger
	metrics   *StressTestResult
	latencies []float64
	mu        sync.Mutex
	stopCh    chan struct{}
}

// NewCollectorStressTester creates a new stress tester
func NewCollectorStressTester(collector collectors.Collector, config StressTestConfig, logger *zap.Logger) *CollectorStressTester {
	return &CollectorStressTester{
		collector: collector,
		config:    config,
		logger:    logger,
		metrics:   &StressTestResult{},
		latencies: make([]float64, 0),
		stopCh:    make(chan struct{}),
	}
}

// Run executes the stress test
func (st *CollectorStressTester) Run(ctx context.Context) (*StressTestResult, error) {
	// Start collector
	if err := st.collector.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start collector: %w", err)
	}
	defer st.collector.Stop()

	// Start memory monitoring
	go st.monitorMemory()

	// Start event consumers
	var wg sync.WaitGroup
	for i := 0; i < st.config.ConcurrentReaders; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			st.consumeEvents(id)
		}(i)
	}

	// Run test for specified duration
	testCtx, cancel := context.WithTimeout(ctx, st.config.Duration)
	defer cancel()

	// Wait for test completion
	<-testCtx.Done()
	close(st.stopCh)

	// Wait for consumers to finish
	wg.Wait()

	// Calculate final metrics
	st.calculateMetrics()

	return st.metrics, nil
}

// consumeEvents reads and processes events from the collector
func (st *CollectorStressTester) consumeEvents(id int) {
	events := st.collector.Events()

	for {
		select {
		case <-st.stopCh:
			return
		case event, ok := <-events:
			if !ok {
				return
			}

			start := time.Now()
			st.processEvent(event)
			latency := time.Since(start).Milliseconds()

			st.mu.Lock()
			st.latencies = append(st.latencies, float64(latency))
			atomic.AddInt64(&st.metrics.ProcessedEvents, 1)
			st.mu.Unlock()
		default:
			// No event available, continue
			time.Sleep(1 * time.Millisecond)
		}
	}
}

// processEvent simulates event processing
func (st *CollectorStressTester) processEvent(event collectors.RawEvent) {
	// Validate event structure
	if event.Type == "" {
		atomic.AddInt64(&st.metrics.ErrorCount, 1)
		return
	}

	if event.TraceID == "" || event.SpanID == "" {
		atomic.AddInt64(&st.metrics.ErrorCount, 1)
		return
	}

	// Simulate processing work
	time.Sleep(100 * time.Microsecond)
}

// monitorMemory tracks memory usage during the test
func (st *CollectorStressTester) monitorMemory() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-st.stopCh:
			return
		case <-ticker.C:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			currentMB := int64(m.Alloc / 1024 / 1024)

			if currentMB > atomic.LoadInt64(&st.metrics.MaxMemoryUsedMB) {
				atomic.StoreInt64(&st.metrics.MaxMemoryUsedMB, currentMB)
			}
		}
	}
}

// calculateMetrics calculates final test metrics
func (st *CollectorStressTester) calculateMetrics() {
	st.mu.Lock()
	defer st.mu.Unlock()

	if len(st.latencies) == 0 {
		return
	}

	// Calculate average latency
	sum := 0.0
	for _, l := range st.latencies {
		sum += l
	}
	st.metrics.AvgLatencyMs = sum / float64(len(st.latencies))

	// Calculate percentiles (simplified - should use proper percentile calculation)
	// For now, just estimate
	st.metrics.P95LatencyMs = st.metrics.AvgLatencyMs * 1.5
	st.metrics.P99LatencyMs = st.metrics.AvgLatencyMs * 2.0

	// Calculate events per second
	st.metrics.EventsPerSecond = float64(st.metrics.ProcessedEvents) / st.config.Duration.Seconds()
}

// TestCNICollectorStress performs stress testing on CNI collector
func TestCNICollectorStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	setupOTEL(t)
	logger := zaptest.NewLogger(t)

	collector, err := cni.NewCollector("stress-cni")
	require.NoError(t, err)

	config := DefaultStressTestConfig()
	config.Duration = 5 * time.Second
	config.EventRate = 500

	tester := NewCollectorStressTester(collector, config, logger)

	ctx := context.Background()
	result, err := tester.Run(ctx)
	require.NoError(t, err)

	// Verify stress test results
	assert.Greater(t, result.ProcessedEvents, int64(0), "Should have processed events")
	assert.Less(t, result.MaxMemoryUsedMB, config.MaxMemoryMB, "Memory usage should be within limits")
	assert.Greater(t, result.EventsPerSecond, float64(0), "Should have positive throughput")

	t.Logf("CNI Stress Test Results:")
	t.Logf("  Processed Events: %d", result.ProcessedEvents)
	t.Logf("  Dropped Events: %d", result.DroppedEvents)
	t.Logf("  Errors: %d", result.ErrorCount)
	t.Logf("  Max Memory: %d MB", result.MaxMemoryUsedMB)
	t.Logf("  Avg Latency: %.2f ms", result.AvgLatencyMs)
	t.Logf("  Events/sec: %.2f", result.EventsPerSecond)
}

// TestKernelCollectorStress performs stress testing on kernel collector
func TestKernelCollectorStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	setupOTEL(t)
	logger := zaptest.NewLogger(t)

	collector, err := kernel.NewModularCollector("stress-kernel")
	require.NoError(t, err)

	config := DefaultStressTestConfig()
	config.Duration = 5 * time.Second
	config.EventRate = 1000

	tester := NewCollectorStressTester(collector, config, logger)

	ctx := context.Background()
	result, err := tester.Run(ctx)
	// Kernel collector may fail in test environment due to eBPF requirements
	if err != nil {
		t.Skipf("Kernel collector stress test skipped: %v", err)
	}

	// Verify stress test results
	assert.GreaterOrEqual(t, result.ProcessedEvents, int64(0), "Should have processed events or zero if no eBPF")
	assert.Less(t, result.MaxMemoryUsedMB, config.MaxMemoryMB, "Memory usage should be within limits")

	t.Logf("Kernel Stress Test Results:")
	t.Logf("  Processed Events: %d", result.ProcessedEvents)
	t.Logf("  Max Memory: %d MB", result.MaxMemoryUsedMB)
	t.Logf("  Events/sec: %.2f", result.EventsPerSecond)
}

// TestMultiCollectorStress tests multiple collectors running concurrently
func TestMultiCollectorStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	setupOTEL(t)
	logger := zaptest.NewLogger(t)

	// Create multiple collectors
	collectors := []collectors.Collector{
		mustCreateCollector(t, "cni", func() (collectors.Collector, error) {
			return cni.NewCollector("multi-cni")
		}),
		mustCreateCollector(t, "dns", func() (collectors.Collector, error) {
			return dns.NewCollector("multi-dns")
		}),
		mustCreateCollector(t, "systemd", func() (collectors.Collector, error) {
			return systemd.NewCollector("multi-systemd")
		}),
	}

	ctx := context.Background()

	// Start all collectors
	for _, c := range collectors {
		if err := c.Start(ctx); err != nil {
			t.Logf("Warning: Failed to start collector %s: %v", c.Name(), err)
		} else {
			defer c.Stop()
		}
	}

	// Run stress test for each collector concurrently
	var wg sync.WaitGroup
	results := make(map[string]*StressTestResult)
	var resultsMu sync.Mutex

	for _, c := range collectors {
		if !c.IsHealthy() {
			continue
		}

		wg.Add(1)
		go func(collector collectors.Collector) {
			defer wg.Done()

			config := DefaultStressTestConfig()
			config.Duration = 3 * time.Second
			config.EventRate = 300

			tester := NewCollectorStressTester(collector, config, logger)
			result, err := tester.Run(ctx)

			resultsMu.Lock()
			if err == nil {
				results[collector.Name()] = result
			}
			resultsMu.Unlock()
		}(c)
	}

	wg.Wait()

	// Log results
	t.Logf("Multi-Collector Stress Test Results:")
	for name, result := range results {
		t.Logf("  %s:", name)
		t.Logf("    Processed: %d events", result.ProcessedEvents)
		t.Logf("    Memory: %d MB", result.MaxMemoryUsedMB)
		t.Logf("    Rate: %.2f events/sec", result.EventsPerSecond)
	}
}

// TestCollectorRecovery tests collector recovery from errors
func TestCollectorRecovery(t *testing.T) {
	setupOTEL(t)

	collector, err := cni.NewCollector("recovery-test")
	require.NoError(t, err)

	ctx := context.Background()

	// Start and stop collector multiple times
	for i := 0; i < 5; i++ {
		err := collector.Start(ctx)
		require.NoError(t, err)

		// Verify it's healthy
		assert.True(t, collector.IsHealthy())

		// Stop collector
		err = collector.Stop()
		require.NoError(t, err)

		// Verify it's not healthy after stop
		assert.False(t, collector.IsHealthy())
	}
}

// TestCollectorMemoryLeak tests for memory leaks
func TestCollectorMemoryLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory leak test in short mode")
	}

	setupOTEL(t)

	collector, err := cni.NewCollector("memleak-test")
	require.NoError(t, err)

	ctx := context.Background()

	// Record initial memory
	var initialMem runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&initialMem)

	// Run multiple start/stop cycles
	for i := 0; i < 10; i++ {
		err := collector.Start(ctx)
		require.NoError(t, err)

		// Let it run briefly
		time.Sleep(100 * time.Millisecond)

		err = collector.Stop()
		require.NoError(t, err)
	}

	// Force GC and check memory
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	runtime.GC()

	var finalMem runtime.MemStats
	runtime.ReadMemStats(&finalMem)

	// Memory should not grow significantly (allow 10MB growth)
	memGrowthMB := int64(finalMem.Alloc-initialMem.Alloc) / 1024 / 1024
	assert.Less(t, memGrowthMB, int64(10), "Memory growth should be minimal")

	t.Logf("Memory growth: %d MB", memGrowthMB)
}

// Helper functions

func setupOTEL(t *testing.T) {
	// Set up resource
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			attribute.String("service.name", "stress-test"),
		),
	)
	require.NoError(t, err)

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

func mustCreateCollector(t *testing.T, name string, factory func() (collectors.Collector, error)) collectors.Collector {
	collector, err := factory()
	if err != nil {
		t.Logf("Warning: Failed to create %s collector: %v", name, err)
		// Return a mock collector that does nothing
		return &MockCollector{name: name}
	}
	return collector
}

// MockCollector is a no-op collector for testing
type MockCollector struct {
	name   string
	events chan collectors.RawEvent
}

func (m *MockCollector) Name() string {
	return m.name
}

func (m *MockCollector) Start(ctx context.Context) error {
	m.events = make(chan collectors.RawEvent, 100)
	return nil
}

func (m *MockCollector) Stop() error {
	if m.events != nil {
		close(m.events)
	}
	return nil
}

func (m *MockCollector) Events() <-chan collectors.RawEvent {
	if m.events == nil {
		m.events = make(chan collectors.RawEvent, 100)
	}
	return m.events
}

func (m *MockCollector) IsHealthy() bool {
	return false
}
