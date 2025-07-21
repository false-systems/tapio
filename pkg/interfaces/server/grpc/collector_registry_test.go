package grpc

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pb "github.com/yairfalse/tapio/proto/gen/tapio/v1"
	"go.uber.org/zap"
)

func TestInMemoryCollectorRegistry_RegisterCollector(t *testing.T) {
	logger := zap.NewNop()
	registry := NewInMemoryCollectorRegistry(logger)
	defer registry.Close()

	info := CollectorInfo{
		Type:         "process",
		Version:      "1.0.0",
		Capabilities: []string{"process-events", "kernel-events"},
		EventTypes:   []string{"process", "kernel"},
		LastSeen:     time.Now(),
	}

	// Test successful registration
	err := registry.RegisterCollector("test-collector", info)
	require.NoError(t, err)

	// Test duplicate registration
	err = registry.RegisterCollector("test-collector", info)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")

	// Verify collector is registered
	collectors := registry.GetCollectors()
	assert.Len(t, collectors, 1)
	assert.Equal(t, info.Type, collectors["test-collector"].Type)
}

func TestInMemoryCollectorRegistry_GetCollectors(t *testing.T) {
	logger := zap.NewNop()
	registry := NewInMemoryCollectorRegistry(logger)
	defer registry.Close()

	// Register multiple collectors
	collectors := []struct {
		name string
		info CollectorInfo
	}{
		{
			name: "collector-1",
			info: CollectorInfo{
				Type:    "process",
				Version: "1.0.0",
			},
		},
		{
			name: "collector-2",
			info: CollectorInfo{
				Type:    "network",
				Version: "2.0.0",
			},
		},
		{
			name: "collector-3",
			info: CollectorInfo{
				Type:    "kernel",
				Version: "1.5.0",
			},
		},
	}

	for _, c := range collectors {
		err := registry.RegisterCollector(c.name, c.info)
		require.NoError(t, err)
	}

	// Get all collectors
	allCollectors := registry.GetCollectors()
	assert.Len(t, allCollectors, 3)

	// Verify each collector
	for _, c := range collectors {
		assert.Contains(t, allCollectors, c.name)
		assert.Equal(t, c.info.Type, allCollectors[c.name].Type)
		assert.Equal(t, c.info.Version, allCollectors[c.name].Version)
	}
}

func TestInMemoryCollectorRegistry_GetCollectorHealth(t *testing.T) {
	logger := zap.NewNop()
	registry := NewInMemoryCollectorRegistry(logger)
	defer registry.Close()

	// Register collector
	info := CollectorInfo{
		Type:     "process",
		Version:  "1.0.0",
		LastSeen: time.Now(),
	}
	err := registry.RegisterCollector("test-collector", info)
	require.NoError(t, err)

	// Get health status
	health, err := registry.GetCollectorHealth("test-collector")
	require.NoError(t, err)
	assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_UNKNOWN, health.Status)

	// Update health
	newHealth := HealthStatus{
		Status:      pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		Message:     "All systems operational",
		LastHealthy: time.Now(),
		Metrics: map[string]float64{
			"events_processed": 100,
		},
	}
	err = registry.UpdateCollectorHealth("test-collector", newHealth)
	require.NoError(t, err)

	// Verify updated health
	health, err = registry.GetCollectorHealth("test-collector")
	require.NoError(t, err)
	assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_HEALTHY, health.Status)
	assert.Equal(t, "All systems operational", health.Message)

	// Test non-existent collector
	_, err = registry.GetCollectorHealth("non-existent")
	assert.Error(t, err)
}

func TestInMemoryCollectorRegistry_GetCollectorMetrics(t *testing.T) {
	logger := zap.NewNop()
	registry := NewInMemoryCollectorRegistry(logger)
	defer registry.Close()

	// Register collector
	info := CollectorInfo{
		Type:    "process",
		Version: "1.0.0",
	}
	err := registry.RegisterCollector("test-collector", info)
	require.NoError(t, err)

	// Update metrics
	metrics := map[string]float64{
		"events_processed": 1000,
		"errors":           5,
		"latency_ms":       25.5,
	}
	err = registry.UpdateCollectorMetrics("test-collector", metrics)
	require.NoError(t, err)

	// Get metrics
	retrievedMetrics, err := registry.GetCollectorMetrics("test-collector")
	require.NoError(t, err)
	assert.Equal(t, metrics, retrievedMetrics)

	// Update with partial metrics
	partialMetrics := map[string]float64{
		"events_processed": 2000,
		"new_metric":       42,
	}
	err = registry.UpdateCollectorMetrics("test-collector", partialMetrics)
	require.NoError(t, err)

	// Verify merge
	retrievedMetrics, err = registry.GetCollectorMetrics("test-collector")
	require.NoError(t, err)
	assert.Equal(t, float64(2000), retrievedMetrics["events_processed"])
	assert.Equal(t, float64(5), retrievedMetrics["errors"])
	assert.Equal(t, float64(42), retrievedMetrics["new_metric"])
}

func TestInMemoryCollectorRegistry_UnregisterCollector(t *testing.T) {
	logger := zap.NewNop()
	registry := NewInMemoryCollectorRegistry(logger)
	defer registry.Close()

	// Register collector
	info := CollectorInfo{
		Type:    "process",
		Version: "1.0.0",
	}
	err := registry.RegisterCollector("test-collector", info)
	require.NoError(t, err)

	// Unregister
	err = registry.UnregisterCollector("test-collector")
	require.NoError(t, err)

	// Verify it's gone
	collectors := registry.GetCollectors()
	assert.Len(t, collectors, 0)

	// Try to unregister again
	err = registry.UnregisterCollector("test-collector")
	assert.Error(t, err)
}

func TestInMemoryCollectorRegistry_GetCollectorsByType(t *testing.T) {
	logger := zap.NewNop()
	registry := NewInMemoryCollectorRegistry(logger)
	defer registry.Close()

	// Register collectors of different types
	collectors := []struct {
		name string
		typ  string
	}{
		{"proc-1", "process"},
		{"proc-2", "process"},
		{"net-1", "network"},
		{"kern-1", "kernel"},
		{"proc-3", "process"},
	}

	for _, c := range collectors {
		info := CollectorInfo{
			Type:    c.typ,
			Version: "1.0.0",
		}
		err := registry.RegisterCollector(c.name, info)
		require.NoError(t, err)
	}

	// Get process collectors
	processCollectors := registry.GetCollectorsByType("process")
	assert.Len(t, processCollectors, 3)
	assert.Contains(t, processCollectors, "proc-1")
	assert.Contains(t, processCollectors, "proc-2")
	assert.Contains(t, processCollectors, "proc-3")

	// Get network collectors
	networkCollectors := registry.GetCollectorsByType("network")
	assert.Len(t, networkCollectors, 1)
	assert.Contains(t, networkCollectors, "net-1")

	// Get non-existent type
	noneCollectors := registry.GetCollectorsByType("non-existent")
	assert.Len(t, noneCollectors, 0)
}

func TestInMemoryCollectorRegistry_GetCollectorsByCapability(t *testing.T) {
	logger := zap.NewNop()
	registry := NewInMemoryCollectorRegistry(logger)
	defer registry.Close()

	// Register collectors with different capabilities
	collectors := []struct {
		name         string
		capabilities []string
	}{
		{"col-1", []string{"process-events", "kernel-events"}},
		{"col-2", []string{"network-events", "dns-events"}},
		{"col-3", []string{"process-events", "memory-events"}},
		{"col-4", []string{"kernel-events", "syscall-events"}},
	}

	for _, c := range collectors {
		info := CollectorInfo{
			Type:         "multi",
			Version:      "1.0.0",
			Capabilities: c.capabilities,
		}
		err := registry.RegisterCollector(c.name, info)
		require.NoError(t, err)
	}

	// Get collectors with process-events capability
	processCapable := registry.GetCollectorsByCapability("process-events")
	assert.Len(t, processCapable, 2)
	assert.Contains(t, processCapable, "col-1")
	assert.Contains(t, processCapable, "col-3")

	// Get collectors with kernel-events capability
	kernelCapable := registry.GetCollectorsByCapability("kernel-events")
	assert.Len(t, kernelCapable, 2)
	assert.Contains(t, kernelCapable, "col-1")
	assert.Contains(t, kernelCapable, "col-4")
}

func TestInMemoryCollectorRegistry_Health(t *testing.T) {
	logger := zap.NewNop()
	registry := NewInMemoryCollectorRegistry(logger)
	defer registry.Close()

	// Initially healthy with no collectors
	health := registry.Health()
	assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_HEALTHY, health.Status)

	// Register collectors with different health statuses
	collectors := []struct {
		name   string
		status pb.HealthStatus_Status
	}{
		{"healthy-1", pb.HealthStatus_HEALTH_STATUS_HEALTHY},
		{"healthy-2", pb.HealthStatus_HEALTH_STATUS_HEALTHY},
		{"degraded-1", pb.HealthStatus_HEALTH_STATUS_DEGRADED},
		{"unhealthy-1", pb.HealthStatus_HEALTH_STATUS_UNHEALTHY},
	}

	for _, c := range collectors {
		info := CollectorInfo{
			Type:    "test",
			Version: "1.0.0",
		}
		err := registry.RegisterCollector(c.name, info)
		require.NoError(t, err)

		healthStatus := HealthStatus{
			Status:  c.status,
			Message: "Test status",
		}
		err = registry.UpdateCollectorHealth(c.name, healthStatus)
		require.NoError(t, err)
	}

	// Registry should be degraded with one unhealthy collector
	health = registry.Health()
	assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_DEGRADED, health.Status)
	assert.Contains(t, health.Message, "degraded")

	// Make majority unhealthy
	for i := 2; i <= 3; i++ {
		healthStatus := HealthStatus{
			Status:  pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
			Message: "Test unhealthy",
		}
		err := registry.UpdateCollectorHealth(collectors[i].name, healthStatus)
		require.NoError(t, err)
	}

	// Registry should be unhealthy
	health = registry.Health()
	assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_UNHEALTHY, health.Status)
}

func TestInMemoryCollectorRegistry_HealthMonitoring(t *testing.T) {
	logger := zap.NewNop()
	registry := &InMemoryCollectorRegistry{
		logger:              logger,
		collectors:          make(map[string]*RegisteredCollector),
		healthCheckInterval: 100 * time.Millisecond,
		healthCheckTimeout:  50 * time.Millisecond,
		shutdown:            make(chan struct{}),
	}

	// Start health monitor
	registry.wg.Add(1)
	go registry.healthMonitor()
	defer registry.Close()

	// Register collector
	info := CollectorInfo{
		Type:     "test",
		Version:  "1.0.0",
		LastSeen: time.Now(),
	}
	err := registry.RegisterCollector("test-collector", info)
	require.NoError(t, err)

	// Wait for health checks
	time.Sleep(350 * time.Millisecond)

	// Collector should be degraded or unhealthy due to no updates
	health, err := registry.GetCollectorHealth("test-collector")
	require.NoError(t, err)
	assert.NotEqual(t, pb.HealthStatus_HEALTH_STATUS_HEALTHY, health.Status)
	assert.Contains(t, health.Message, "update")
}

func TestInMemoryCollectorRegistry_GetStatistics(t *testing.T) {
	logger := zap.NewNop()
	registry := NewInMemoryCollectorRegistry(logger)
	defer registry.Close()

	// Register collectors of different types
	types := []string{"process", "process", "network", "kernel", "process"}
	for i, typ := range types {
		info := CollectorInfo{
			Type:    typ,
			Version: "1.0.0",
		}
		err := registry.RegisterCollector("collector-"+string(rune('a'+i)), info)
		require.NoError(t, err)
	}

	// Update some health statuses
	statuses := []pb.HealthStatus_Status{
		pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		pb.HealthStatus_HEALTH_STATUS_DEGRADED,
		pb.HealthStatus_HEALTH_STATUS_UNHEALTHY,
		pb.HealthStatus_HEALTH_STATUS_HEALTHY,
	}

	for i, status := range statuses {
		healthStatus := HealthStatus{
			Status:  status,
			Message: "Test",
		}
		err := registry.UpdateCollectorHealth("collector-"+string(rune('a'+i)), healthStatus)
		require.NoError(t, err)
	}

	// Get statistics
	stats := registry.GetStatistics()

	assert.Equal(t, 5, stats["total_collectors"])
	assert.Equal(t, int64(5), stats["registration_count"])

	// Check type distribution
	typeCount := stats["collectors_by_type"].(map[string]int)
	assert.Equal(t, 3, typeCount["process"])
	assert.Equal(t, 1, typeCount["network"])
	assert.Equal(t, 1, typeCount["kernel"])

	// Check status distribution
	statusCount := stats["collectors_by_status"].(map[string]int)
	assert.Equal(t, 3, statusCount[pb.HealthStatus_HEALTH_STATUS_HEALTHY.String()])
	assert.Equal(t, 1, statusCount[pb.HealthStatus_HEALTH_STATUS_DEGRADED.String()])
	assert.Equal(t, 1, statusCount[pb.HealthStatus_HEALTH_STATUS_UNHEALTHY.String()])
}

func TestInMemoryCollectorRegistry_GetCollectorStatistics(t *testing.T) {
	logger := zap.NewNop()
	registry := NewInMemoryCollectorRegistry(logger)
	defer registry.Close()

	// Register collector
	info := CollectorInfo{
		Type:         "process",
		Version:      "1.0.0",
		Capabilities: []string{"process-events", "kernel-events"},
		EventTypes:   []string{"process", "kernel"},
		LastSeen:     time.Now(),
	}
	err := registry.RegisterCollector("test-collector", info)
	require.NoError(t, err)

	// Update health and metrics
	healthStatus := HealthStatus{
		Status:      pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		Message:     "Running smoothly",
		LastHealthy: time.Now(),
	}
	err = registry.UpdateCollectorHealth("test-collector", healthStatus)
	require.NoError(t, err)

	metrics := map[string]float64{
		"events_processed": 1000,
		"errors":           5,
	}
	err = registry.UpdateCollectorMetrics("test-collector", metrics)
	require.NoError(t, err)

	// Get statistics
	stats, err := registry.GetCollectorStatistics("test-collector")
	require.NoError(t, err)

	assert.Equal(t, "test-collector", stats.Name)
	assert.Equal(t, "process", stats.Type)
	assert.Equal(t, pb.HealthStatus_HEALTH_STATUS_HEALTHY.String(), stats.Status)
	assert.Equal(t, info.Capabilities, stats.Capabilities)
	assert.Equal(t, info.EventTypes, stats.EventTypes)
	assert.Equal(t, metrics, stats.Metrics)
	assert.Greater(t, stats.UptimeSeconds, float64(0))

	// Test non-existent collector
	_, err = registry.GetCollectorStatistics("non-existent")
	assert.Error(t, err)
}

func TestInMemoryCollectorRegistry_ConcurrentAccess(t *testing.T) {
	logger := zap.NewNop()
	registry := NewInMemoryCollectorRegistry(logger)
	defer registry.Close()

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Concurrent registrations
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			info := CollectorInfo{
				Type:    "concurrent",
				Version: "1.0.0",
			}
			err := registry.RegisterCollector("concurrent-"+string(rune('a'+id)), info)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	// Concurrent health updates
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Wait a bit for registration
			time.Sleep(10 * time.Millisecond)

			health := HealthStatus{
				Status:  pb.HealthStatus_HEALTH_STATUS_HEALTHY,
				Message: "Concurrent update",
			}
			err := registry.UpdateCollectorHealth("concurrent-"+string(rune('a'+id)), health)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = registry.GetCollectors()
			_ = registry.GetCollectorsByType("concurrent")
			_ = registry.Health()
		}()
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent operation error: %v", err)
	}

	// Verify final state
	collectors := registry.GetCollectors()
	assert.Len(t, collectors, 10)
}

// Benchmarks
func BenchmarkCollectorRegistry_Register(b *testing.B) {
	logger := zap.NewNop()
	registry := NewInMemoryCollectorRegistry(logger)
	defer registry.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		info := CollectorInfo{
			Type:    "benchmark",
			Version: "1.0.0",
		}
		registry.RegisterCollector("bench-"+string(rune(i)), info)
	}
}

func BenchmarkCollectorRegistry_GetCollectors(b *testing.B) {
	logger := zap.NewNop()
	registry := NewInMemoryCollectorRegistry(logger)
	defer registry.Close()

	// Pre-populate
	for i := 0; i < 100; i++ {
		info := CollectorInfo{
			Type:    "benchmark",
			Version: "1.0.0",
		}
		registry.RegisterCollector("collector-"+string(rune(i)), info)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = registry.GetCollectors()
	}
}

func BenchmarkCollectorRegistry_UpdateHealth(b *testing.B) {
	logger := zap.NewNop()
	registry := NewInMemoryCollectorRegistry(logger)
	defer registry.Close()

	// Register collector
	info := CollectorInfo{
		Type:    "benchmark",
		Version: "1.0.0",
	}
	registry.RegisterCollector("bench-collector", info)

	health := HealthStatus{
		Status:      pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		Message:     "Benchmark",
		LastHealthy: time.Now(),
		Metrics: map[string]float64{
			"metric1": 1.0,
			"metric2": 2.0,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		registry.UpdateCollectorHealth("bench-collector", health)
	}
}
