package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
	"k8s.io/client-go/kubernetes/fake"
)

// TestNewSchedulerObserver_Minimal tests creating observer with minimal config
func TestNewSchedulerObserver_Minimal(t *testing.T) {
	config := Config{
		SchedulerMetricsURL: "http://localhost:10251/metrics",
		ScrapeInterval:      30 * time.Second,
	}

	obs, err := NewSchedulerObserver("test-scheduler", config)
	require.NoError(t, err)
	require.NotNil(t, obs)

	// Verify base observer
	assert.NotNil(t, obs.BaseObserver)

	// Verify Prometheus scraper created
	assert.NotNil(t, obs.promScraper)

	// Verify OTEL metrics created
	assert.NotNil(t, obs.schedulingAttemptsTotal)
	assert.NotNil(t, obs.schedulingErrorsTotal)
	assert.NotNil(t, obs.pendingPodsGauge)
	assert.NotNil(t, obs.preemptionEventsTotal)
	assert.NotNil(t, obs.pluginDurationMs)
}

// TestNewSchedulerObserver_WithK8sClient tests observer with K8s client
func TestNewSchedulerObserver_WithK8sClient(t *testing.T) {
	config := Config{
		SchedulerMetricsURL: "http://localhost:10251/metrics",
		ScrapeInterval:      30 * time.Second,
		K8sClientset:        fake.NewSimpleClientset(),
	}

	obs, err := NewSchedulerObserver("test-scheduler", config)
	require.NoError(t, err)
	require.NotNil(t, obs)

	// Verify Events API watcher created
	assert.NotNil(t, obs.eventsWatcher)
}

// TestNewSchedulerObserver_WithEmitter tests observer with emitter
func TestNewSchedulerObserver_WithEmitter(t *testing.T) {
	config := Config{
		SchedulerMetricsURL: "http://localhost:10251/metrics",
		ScrapeInterval:      30 * time.Second,
		Emitter:             base.NewStdoutEmitter(),
	}

	obs, err := NewSchedulerObserver("test-scheduler", config)
	require.NoError(t, err)
	require.NotNil(t, obs)

	assert.NotNil(t, obs.emitter)
}

// TestSchedulerObserver_StartStop tests lifecycle
func TestSchedulerObserver_StartStop(t *testing.T) {
	config := Config{
		SchedulerMetricsURL: "http://localhost:10251/metrics",
		ScrapeInterval:      30 * time.Second,
	}

	obs, err := NewSchedulerObserver("test-scheduler", config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start observer in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- obs.Start(ctx)
	}()

	// Give observer time to start
	time.Sleep(100 * time.Millisecond)

	// Stop observer
	err = obs.Stop()
	assert.NoError(t, err)

	// Wait for Start to complete
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(1 * time.Second):
		t.Fatal("observer did not stop within timeout")
	}
}

// TestSchedulerObserver_StartStop_WithEventsWatcher tests lifecycle with Events API watcher
func TestSchedulerObserver_StartStop_WithEventsWatcher(t *testing.T) {
	config := Config{
		SchedulerMetricsURL: "http://localhost:10251/metrics",
		ScrapeInterval:      30 * time.Second,
		K8sClientset:        fake.NewSimpleClientset(), // Adds Events API watcher
	}

	obs, err := NewSchedulerObserver("test-scheduler", config)
	require.NoError(t, err)
	require.NotNil(t, obs.eventsWatcher, "Events watcher should be created with K8s client")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start observer in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- obs.Start(ctx)
	}()

	// Give observer time to start
	time.Sleep(100 * time.Millisecond)

	// Stop observer
	err = obs.Stop()
	assert.NoError(t, err)

	// Wait for Start to complete or timeout
	select {
	case err := <-errCh:
		// With fake clientset, Events API watcher will fail to sync cache
		// This is expected behavior - we just want to verify lifecycle works
		// Accept: nil (clean shutdown), context.Canceled, or "failed to sync Events cache"
		if err != nil && err != context.Canceled {
			assert.Contains(t, err.Error(), "failed to sync Events cache",
				"expected cache sync error with fake clientset, got: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("observer did not stop within timeout")
	}
}
