package base

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/metric"
)

// setupOTEL sets up OTEL for tests
func setupOTEL(t *testing.T) {
	t.Helper()
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(nil)
	})
}

func TestNewBaseObserver(t *testing.T) {
	setupOTEL(t)

	obs, err := NewBaseObserver("test-observer")
	require.NoError(t, err)
	require.NotNil(t, obs)

	assert.Equal(t, "test-observer", obs.Name())
	assert.NotNil(t, obs.metrics)
	assert.NotNil(t, obs.pipeline)
	assert.False(t, obs.IsHealthy()) // Not running yet
}

func TestBaseObserver_Name(t *testing.T) {
	setupOTEL(t)
	obs, err := NewBaseObserver("my-observer")
	require.NoError(t, err)

	assert.Equal(t, "my-observer", obs.Name())
}

func TestBaseObserver_IsHealthy(t *testing.T) {
	setupOTEL(t)
	obs, err := NewBaseObserver("test-observer")
	require.NoError(t, err)

	// Not running initially
	assert.False(t, obs.IsHealthy())

	// Start running
	obs.running.Store(true)
	assert.True(t, obs.IsHealthy())

	// Stop
	obs.stopped.Store(true)
	assert.False(t, obs.IsHealthy())
}

func TestBaseObserver_Start_Success(t *testing.T) {
	setupOTEL(t)
	obs, err := NewBaseObserver("test-observer")
	require.NoError(t, err)

	// Add a simple stage
	called := false
	obs.AddStage(func(ctx context.Context) error {
		called = true
		return nil
	})

	ctx := context.Background()
	err = obs.Start(ctx)

	require.NoError(t, err)
	assert.True(t, called)
}

func TestBaseObserver_Start_AlreadyRunning(t *testing.T) {
	setupOTEL(t)
	obs, err := NewBaseObserver("test-observer")
	require.NoError(t, err)

	obs.running.Store(true)

	ctx := context.Background()
	err = obs.Start(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestBaseObserver_Start_PipelineFailure(t *testing.T) {
	setupOTEL(t)
	obs, err := NewBaseObserver("test-observer")
	require.NoError(t, err)

	// Add a failing stage
	obs.AddStage(func(ctx context.Context) error {
		return assert.AnError
	})

	ctx := context.Background()
	err = obs.Start(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pipeline failed")
	assert.False(t, obs.running.Load())
	assert.True(t, obs.stopped.Load())
}

func TestBaseObserver_Stop(t *testing.T) {
	setupOTEL(t)
	obs, err := NewBaseObserver("test-observer")
	require.NoError(t, err)

	obs.running.Store(true)

	err = obs.Stop()
	require.NoError(t, err)

	assert.False(t, obs.running.Load())
	assert.True(t, obs.stopped.Load())
}

func TestBaseObserver_Stop_NotRunning(t *testing.T) {
	setupOTEL(t)
	obs, err := NewBaseObserver("test-observer")
	require.NoError(t, err)

	err = obs.Stop()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestBaseObserver_AddStage(t *testing.T) {
	setupOTEL(t)
	obs, err := NewBaseObserver("test-observer")
	require.NoError(t, err)

	stage1Called := false
	stage2Called := false

	obs.AddStage(func(ctx context.Context) error {
		stage1Called = true
		return nil
	})

	obs.AddStage(func(ctx context.Context) error {
		stage2Called = true
		return nil
	})

	ctx := context.Background()
	err = obs.Start(ctx)

	require.NoError(t, err)
	assert.True(t, stage1Called)
	assert.True(t, stage2Called)
}

func TestBaseObserver_RecordEvent(t *testing.T) {
	setupOTEL(t)
	obs, err := NewBaseObserver("test-observer")
	require.NoError(t, err)

	ctx := context.Background()
	event := &domain.ObserverEvent{
		ID:        "test-1",
		Type:      "tcp_connect",
		Source:    "test-observer",
		Timestamp: time.Now(),
	}

	assert.Equal(t, int64(0), obs.eventsProcessed.Load())

	obs.RecordEvent(ctx, event)
	assert.Equal(t, int64(1), obs.eventsProcessed.Load())

	obs.RecordEvent(ctx, event)
	obs.RecordEvent(ctx, event)
	assert.Equal(t, int64(3), obs.eventsProcessed.Load())
}

func TestBaseObserver_RecordDrop(t *testing.T) {
	setupOTEL(t)
	obs, err := NewBaseObserver("test-observer")
	require.NoError(t, err)

	ctx := context.Background()

	assert.Equal(t, int64(0), obs.eventsDropped.Load())

	obs.RecordDrop(ctx, "tcp_connect")
	assert.Equal(t, int64(1), obs.eventsDropped.Load())

	obs.RecordDrop(ctx, "tcp_connect")
	assert.Equal(t, int64(2), obs.eventsDropped.Load())
}

func TestBaseObserver_RecordError(t *testing.T) {
	setupOTEL(t)
	obs, err := NewBaseObserver("test-observer")
	require.NoError(t, err)

	ctx := context.Background()
	event := &domain.ObserverEvent{
		ID:        "test-1",
		Type:      "oom_kill",
		Source:    "test-observer",
		Timestamp: time.Now(),
	}

	assert.Equal(t, int64(0), obs.errorsTotal.Load())

	obs.RecordError(ctx, event)
	assert.Equal(t, int64(1), obs.errorsTotal.Load())

	obs.RecordError(ctx, event)
	obs.RecordError(ctx, event)
	assert.Equal(t, int64(3), obs.errorsTotal.Load())
}

func TestBaseObserver_RecordProcessingTime(t *testing.T) {
	setupOTEL(t)
	obs, err := NewBaseObserver("test-observer")
	require.NoError(t, err)

	ctx := context.Background()
	event := &domain.ObserverEvent{
		ID:        "test-1",
		Type:      "tcp_connect",
		Source:    "test-observer",
		Timestamp: time.Now(),
	}

	// Should not panic
	obs.RecordProcessingTime(ctx, event, 10.5)
	obs.RecordProcessingTime(ctx, event, 20.3)
}

func TestBaseObserver_Stats(t *testing.T) {
	setupOTEL(t)
	obs, err := NewBaseObserver("test-observer")
	require.NoError(t, err)

	ctx := context.Background()
	event := &domain.ObserverEvent{
		ID:        "test-1",
		Type:      "tcp_connect",
		Source:    "test-observer",
		Timestamp: time.Now(),
	}

	obs.RecordEvent(ctx, event)
	obs.RecordEvent(ctx, event)
	obs.RecordEvent(ctx, event)
	obs.RecordDrop(ctx, "tcp_connect")
	obs.RecordError(ctx, event)
	obs.RecordError(ctx, event)

	time.Sleep(10 * time.Millisecond)

	stats := obs.Stats()

	assert.Equal(t, "test-observer", stats.Name)
	assert.Greater(t, stats.Uptime, time.Duration(0))
	assert.Equal(t, int64(3), stats.EventsProcessed)
	assert.Equal(t, int64(1), stats.EventsDropped)
	assert.Equal(t, int64(2), stats.ErrorsTotal)
}

func TestBaseObserver_Lifecycle(t *testing.T) {
	setupOTEL(t)
	obs, err := NewBaseObserver("test-observer")
	require.NoError(t, err)

	// Initially not healthy
	assert.False(t, obs.IsHealthy())

	// Add a stage that runs for a short time
	obs.AddStage(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	// Start observer
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go func() {
		_ = obs.Start(ctx) // Ignore: test goroutine error
	}()

	// Wait for observer to be running
	time.Sleep(10 * time.Millisecond)
	assert.True(t, obs.IsHealthy())

	// Stop observer
	err = obs.Stop()
	require.NoError(t, err)
	assert.False(t, obs.IsHealthy())
}
