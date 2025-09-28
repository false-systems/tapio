package kernel

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// TestIntegrationMultipleObservers tests running multiple kernel observers
func TestIntegrationMultipleObservers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t)
	numObservers := 3
	observers := make([]*Observer, numObservers)

	// Create multiple observers
	for i := 0; i < numObservers; i++ {
		config := &Config{
			Name:       fmt.Sprintf("kernel-%d", i),
			BufferSize: 1000,
			EnableEBPF: false,
		}

		observer, err := NewObserverWithConfig(config, logger)
		require.NoError(t, err)
		observers[i] = observer
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start all observers
	for i, observer := range observers {
		err := observer.Start(ctx)
		require.NoError(t, err, "Failed to start observer %d", i)
		defer observer.Stop()
	}

	// Verify all are healthy
	for i, observer := range observers {
		health := observer.Health()
		assert.Equal(t, domain.HealthHealthy, health.Status, "Observer %d not healthy", i)
	}

	// Generate file operations
	tempDir, err := os.MkdirTemp("", "integration-multi")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create some test files
	for i := 0; i < 10; i++ {
		filename := filepath.Join(tempDir, fmt.Sprintf("file%d.txt", i))
		err := os.WriteFile(filename, []byte(fmt.Sprintf("data-%d", i)), 0644)
		require.NoError(t, err)
	}

	// Stop all observers
	for _, observer := range observers {
		err := observer.Stop()
		assert.NoError(t, err)
	}

	// Check final statistics
	for i, observer := range observers {
		stats := observer.Statistics()
		t.Logf("Observer %d stats: Events=%d, Errors=%d",
			i, stats.EventsProcessed, stats.ErrorCount)
	}
}

// TestIntegrationConcurrentEventProcessing tests concurrent event handling
func TestIntegrationConcurrentEventProcessing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Setenv("TAPIO_MOCK_MODE", "true")

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "concurrent-test",
		BufferSize: 1000,
		EnableEBPF: false,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	events := observer.Events()

	// Concurrent event consumers
	var wg sync.WaitGroup
	numConsumers := 5
	eventCounts := make([]int64, numConsumers)

	for i := 0; i < numConsumers; i++ {
		wg.Add(1)
		go func(consumerID int) {
			defer wg.Done()
			timeout := time.After(5 * time.Second)
			for {
				select {
				case event := <-events:
					if event != nil {
						atomic.AddInt64(&eventCounts[consumerID], 1)
					}
				case <-timeout:
					return
				}
			}
		}(i)
	}

	// Wait for consumers
	wg.Wait()

	// Check results
	totalEvents := int64(0)
	for i, count := range eventCounts {
		totalEvents += count
		t.Logf("Consumer %d processed %d events", i, count)
	}

	assert.Greater(t, totalEvents, int64(0), "Should have processed some events")

	// Verify observer health
	health := observer.Health()
	assert.Equal(t, domain.HealthHealthy, health.Status)
}

// TestIntegrationRealKubernetesScenario simulates Kubernetes config monitoring
func TestIntegrationRealKubernetesScenario(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "k8s-scenario",
		BufferSize: 5000,
		EnableEBPF: runtime.GOOS == "linux" && os.Geteuid() == 0,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Create Kubernetes-like directory structure
	baseDir, err := os.MkdirTemp("", "k8s-integration")
	require.NoError(t, err)
	defer os.RemoveAll(baseDir)

	// Simulate multiple pods with configs
	numPods := 5
	for i := 0; i < numPods; i++ {
		podUID := fmt.Sprintf("pod-%d-%s", i, generateUID())

		// ConfigMap directory
		configMapDir := filepath.Join(baseDir, "pods", podUID,
			"volumes", "kubernetes.io~configmap", fmt.Sprintf("app-config-%d", i))
		err := os.MkdirAll(configMapDir, 0755)
		require.NoError(t, err)

		// Secret directory
		secretDir := filepath.Join(baseDir, "pods", podUID,
			"volumes", "kubernetes.io~secret", fmt.Sprintf("app-secret-%d", i))
		err = os.MkdirAll(secretDir, 0755)
		require.NoError(t, err)

		// Create config files
		configFile := filepath.Join(configMapDir, "application.yaml")
		err = os.WriteFile(configFile, []byte(fmt.Sprintf("app: config-%d", i)), 0644)
		require.NoError(t, err)

		secretFile := filepath.Join(secretDir, "password.txt")
		err = os.WriteFile(secretFile, []byte(fmt.Sprintf("secret-%d", i)), 0600)
		require.NoError(t, err)
	}

	// Simulate pod operations
	var accessCount int64
	done := make(chan bool)

	go func() {
		for i := 0; i < 10; i++ {
			podNum := rand.Intn(numPods)
			podUID := fmt.Sprintf("pod-%d-", podNum)

			// Read config
			configPath := filepath.Join(baseDir, "pods")
			dirs, _ := os.ReadDir(configPath)
			for _, dir := range dirs {
				if len(dir.Name()) > len(podUID) && dir.Name()[:len(podUID)] == podUID {
					configFile := filepath.Join(configPath, dir.Name(),
						"volumes", "kubernetes.io~configmap",
						fmt.Sprintf("app-config-%d", podNum), "application.yaml")
					if _, err := os.ReadFile(configFile); err == nil {
						atomic.AddInt64(&accessCount, 1)
					}
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		done <- true
	}()

	// Wait for operations
	<-done

	// Collect some events
	events := observer.Events()
	collectedEvents := make([]*domain.CollectorEvent, 0)
	timeout := time.After(2 * time.Second)

	for {
		select {
		case event := <-events:
			if event != nil {
				collectedEvents = append(collectedEvents, event)
			}
		case <-timeout:
			goto done
		}
	}
done:

	// Log results
	t.Logf("K8s scenario completed:")
	t.Logf("  Access operations: %d", accessCount)
	t.Logf("  Events collected: %d", len(collectedEvents))

	stats := observer.Statistics()
	t.Logf("  Observer stats: Processed=%d, Errors=%d",
		stats.EventsProcessed, stats.ErrorCount)

	// Verify health
	health := observer.Health()
	assert.Equal(t, domain.HealthHealthy, health.Status)
}

// TestIntegrationObserverRestart tests observer restart capabilities
func TestIntegrationObserverRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "restart-test",
		BufferSize: 100,
		EnableEBPF: false,
	}

	// First lifecycle
	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	ctx1 := context.Background()
	err = observer.Start(ctx1)
	require.NoError(t, err)

	// Collect initial stats
	time.Sleep(500 * time.Millisecond)
	stats1 := observer.Statistics()

	// Stop first lifecycle
	err = observer.Stop()
	require.NoError(t, err)

	// Restart observer
	ctx2 := context.Background()
	err = observer.Start(ctx2)
	require.NoError(t, err)

	// Run for a bit
	time.Sleep(500 * time.Millisecond)

	// Stop second lifecycle
	err = observer.Stop()
	require.NoError(t, err)

	// Collect final stats
	stats2 := observer.Statistics()

	t.Logf("First lifecycle stats: Events=%d, Errors=%d",
		stats1.EventsProcessed, stats1.ErrorCount)
	t.Logf("Second lifecycle stats: Events=%d, Errors=%d",
		stats2.EventsProcessed, stats2.ErrorCount)

	// Stats should accumulate across restarts
	assert.GreaterOrEqual(t, stats2.EventsProcessed, stats1.EventsProcessed)
}

// TestIntegrationEventValidation tests event structure validation
func TestIntegrationEventValidation(t *testing.T) {
	t.Setenv("TAPIO_MOCK_MODE", "true")
	t.Setenv("TAPIO_STRICT_VALIDATION", "true")

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "validation-test",
		BufferSize: 100,
		EnableEBPF: false,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	events := observer.Events()
	timeout := time.After(5 * time.Second)
	validEventCount := 0

	for validEventCount < 2 {
		select {
		case event := <-events:
			if event != nil {
				// Validate event structure
				assert.NotEmpty(t, event.EventID)
				assert.NotZero(t, event.Timestamp)
				assert.Equal(t, "validation-test", event.Source)
				assert.Equal(t, domain.EventTypeKernelSyscall, event.Type)
				assert.NotNil(t, event.EventData.Kernel)
				assert.NotZero(t, event.EventData.Kernel.PID)
				assert.NotEmpty(t, event.EventData.Kernel.Command)
				assert.Contains(t, event.Metadata.Labels, "observer")
				assert.Equal(t, "kernel", event.Metadata.Labels["observer"])

				validEventCount++
			}
		case <-timeout:
			if validEventCount == 0 {
				t.Fatal("No valid events received")
			}
			return
		}
	}

	t.Logf("Validated %d events successfully", validEventCount)
}

// TestIntegrationMemoryPressure tests behavior under memory pressure
func TestIntegrationMemoryPressure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Setenv("TAPIO_MOCK_MODE", "true")

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "memory-test",
		BufferSize: 10, // Small buffer to simulate pressure
		EnableEBPF: false,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Don't consume events to create backpressure
	time.Sleep(8 * time.Second) // Let mock events accumulate

	// Now consume events
	events := observer.Events()
	consumedCount := 0
	timeout := time.After(2 * time.Second)

consumeLoop:
	for {
		select {
		case event := <-events:
			if event != nil {
				consumedCount++
			}
		case <-timeout:
			break consumeLoop
		}
	}

	// Check statistics
	stats := observer.Statistics()
	t.Logf("Memory pressure test:")
	t.Logf("  Events consumed: %d", consumedCount)
	t.Logf("  Events processed: %d", stats.EventsProcessed)
	t.Logf("  Errors: %d", stats.ErrorCount)

	// Observer should remain healthy despite drops
	health := observer.Health()
	assert.Equal(t, domain.HealthHealthy, health.Status)

	// Should have some events despite buffer pressure
	assert.Greater(t, consumedCount, 0)
}

// TestIntegrationLongRunningStability tests long-running stability
func TestIntegrationLongRunningStability(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping long-running integration test")
	}

	t.Setenv("TAPIO_MOCK_MODE", "true")

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "stability-test",
		BufferSize: 1000,
		EnableEBPF: false,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	events := observer.Events()

	// Run for extended period
	testDuration := 30 * time.Second
	endTime := time.Now().Add(testDuration)
	eventCount := int64(0)
	healthChecks := 0

	for time.Now().Before(endTime) {
		select {
		case event := <-events:
			if event != nil {
				atomic.AddInt64(&eventCount, 1)
			}
		default:
			// Periodic health check
			if healthChecks%10 == 0 {
				health := observer.Health()
				assert.Equal(t, domain.HealthHealthy, health.Status)
			}
			healthChecks++
			time.Sleep(100 * time.Millisecond)
		}
	}

	finalEvents := atomic.LoadInt64(&eventCount)
	stats := observer.Statistics()

	t.Logf("Long-running stability test completed:")
	t.Logf("  Duration: %v", testDuration)
	t.Logf("  Events collected: %d", finalEvents)
	t.Logf("  Events processed: %d", stats.EventsProcessed)
	t.Logf("  Errors: %d", stats.ErrorCount)
	t.Logf("  Health checks: %d", healthChecks)

	// Should have steady event flow
	expectedMinEvents := int64(testDuration.Seconds() / 3) // Mock generates every 3 seconds
	assert.GreaterOrEqual(t, finalEvents, expectedMinEvents-2,
		"Should have received at least %d events", expectedMinEvents-2)
}

// TestIntegrationContextPropagation tests context propagation
func TestIntegrationContextPropagation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "context-test",
		BufferSize: 100,
		EnableEBPF: false,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = observer.Start(ctx)
	require.NoError(t, err)

	// Wait for context to expire
	<-ctx.Done()
	time.Sleep(500 * time.Millisecond)

	// Stop should work gracefully after context cancellation
	err = observer.Stop()
	assert.NoError(t, err)

	// Health should reflect stopped state
	health := observer.Health()
	// After stop, health might be degraded
	t.Logf("Health after context timeout: %s", health.Status)
}

// Helper function to generate UIDs
func generateUID() string {
	chars := "abcdefghijklmnopqrstuvwxyz0123456789"
	uid := make([]byte, 10)
	for i := range uid {
		uid[i] = chars[rand.Intn(len(chars))]
	}
	return string(uid)
}
