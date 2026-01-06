package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/intelligence"
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
