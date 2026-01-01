package scheduler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/intelligence"
	"k8s.io/client-go/kubernetes/fake"
)

// TestNew_Minimal tests creating observer with minimal config
func TestNew_Minimal(t *testing.T) {
	config := Config{
		SchedulerMetricsURL: "http://localhost:10251/metrics",
		ScrapeInterval:      30 * time.Second,
	}
	deps := base.NewDeps(nil, nil)

	obs, err := New(config, deps)
	require.NoError(t, err)
	require.NotNil(t, obs)

	// Verify fields
	assert.Equal(t, "scheduler", obs.name)
	assert.NotNil(t, obs.deps)

	// Verify Prometheus scraper created
	assert.NotNil(t, obs.promScraper)

	// Verify metrics created
	assert.NotNil(t, obs.schedulingAttemptsTotal)
	assert.NotNil(t, obs.schedulingErrorsTotal)
	assert.NotNil(t, obs.pendingPodsGauge)
	assert.NotNil(t, obs.preemptionEventsTotal)
	assert.NotNil(t, obs.pluginDurationMs)
}

// TestNew_WithK8sClient tests observer with K8s client
func TestNew_WithK8sClient(t *testing.T) {
	config := Config{
		SchedulerMetricsURL: "http://localhost:10251/metrics",
		ScrapeInterval:      30 * time.Second,
		K8sClientset:        fake.NewSimpleClientset(),
	}
	deps := base.NewDeps(nil, nil)

	obs, err := New(config, deps)
	require.NoError(t, err)
	require.NotNil(t, obs)

	// Verify Events API watcher created
	assert.NotNil(t, obs.eventsWatcher)
}

// TestNew_WithEmitter tests observer with emitter
func TestNew_WithEmitter(t *testing.T) {
	config := Config{
		SchedulerMetricsURL: "http://localhost:10251/metrics",
		ScrapeInterval:      30 * time.Second,
	}
	emitter := intelligence.NewMock()
	deps := base.NewDeps(nil, emitter)

	obs, err := New(config, deps)
	require.NoError(t, err)
	require.NotNil(t, obs)

	assert.NotNil(t, obs.deps.Emitter)
}

// TestSchedulerObserver_Run tests lifecycle
func TestSchedulerObserver_Run(t *testing.T) {
	config := Config{
		SchedulerMetricsURL: "http://localhost:10251/metrics",
		ScrapeInterval:      30 * time.Second,
	}
	deps := base.NewDeps(nil, nil)

	obs, err := New(config, deps)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Run blocks until context cancelled
	err = obs.Run(ctx)
	assert.NoError(t, err)
}

// TestSchedulerObserver_Run_WithEventsWatcher tests lifecycle with Events API watcher
func TestSchedulerObserver_Run_WithEventsWatcher(t *testing.T) {
	config := Config{
		SchedulerMetricsURL: "http://localhost:10251/metrics",
		ScrapeInterval:      30 * time.Second,
		K8sClientset:        fake.NewSimpleClientset(), // Adds Events API watcher
	}
	deps := base.NewDeps(nil, nil)

	obs, err := New(config, deps)
	require.NoError(t, err)
	require.NotNil(t, obs.eventsWatcher, "Events watcher should be created with K8s client")

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Run in background
	errCh := make(chan error, 1)
	go func() {
		errCh <- obs.Run(ctx)
	}()

	// Wait for completion or timeout
	select {
	case err := <-errCh:
		// Expected outcomes:
		// - nil: clean shutdown
		// - context.Canceled/DeadlineExceeded: normal timeout
		// - cache sync failure: fake.NewSimpleClientset() doesn't support informer sync
		if err != nil && err != context.Canceled && err != context.DeadlineExceeded && !strings.Contains(err.Error(), "cache") {
			t.Errorf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("observer did not stop within timeout")
	}
}
