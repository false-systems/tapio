package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/base"
	"github.com/yairfalse/tapio/pkg/domain"
	"k8s.io/client-go/kubernetes/fake"
)

// TestEventsWatcher_Run_BlocksUntilCancelled verifies Run() is blocking
func TestEventsWatcher_Run_BlocksUntilCancelled(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	baseObs, err := base.NewBaseObserver("test-scheduler")
	require.NoError(t, err)

	obs := &SchedulerObserver{
		BaseObserver: baseObs,
		emitter:      &mockEmitter{events: make([]*domain.ObserverEvent, 0)},
	}

	watcher := NewEventsWatcher(clientset, obs)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Track if Run() returned
	var wg sync.WaitGroup
	var runReturned bool
	var runErr error
	mu := sync.Mutex{}

	wg.Add(1)
	go func() {
		defer wg.Done()
		runErr = watcher.Run(ctx)
		mu.Lock()
		runReturned = true
		mu.Unlock()
	}()

	// Give Run() time to start
	time.Sleep(100 * time.Millisecond)

	// Verify Run() hasn't returned yet (still blocking)
	mu.Lock()
	assert.False(t, runReturned, "Run() should block until context cancelled")
	mu.Unlock()

	// Cancel context
	cancel()

	// Wait for Run() to return
	wg.Wait()

	// Verify Run() returned after cancellation
	mu.Lock()
	assert.True(t, runReturned, "Run() should return after context cancelled")
	assert.NoError(t, runErr, "Run() should return nil on clean shutdown")
	mu.Unlock()
}

// TestEventsWatcher_Run_ReturnsImmediatelyIfContextAlreadyCancelled verifies fast shutdown
func TestEventsWatcher_Run_ReturnsImmediatelyIfContextAlreadyCancelled(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	baseObs, err := base.NewBaseObserver("test-scheduler")
	require.NoError(t, err)

	obs := &SchedulerObserver{
		BaseObserver: baseObs,
		emitter:      &mockEmitter{events: make([]*domain.ObserverEvent, 0)},
	}

	watcher := NewEventsWatcher(clientset, obs)

	// Create already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Run() should return quickly (with error since cache can't sync)
	start := time.Now()
	err = watcher.Run(ctx)
	duration := time.Since(start)

	assert.Error(t, err, "Run() should return error when cache sync fails on cancelled context")
	assert.Contains(t, err.Error(), "cache", "Error should mention cache sync failure")
	assert.Less(t, duration, 1*time.Second, "Run() should return quickly if context already cancelled")
}

// TestEventsWatcher_Run_CacheSyncFailure verifies error propagation
func TestEventsWatcher_Run_CacheSyncFailure(t *testing.T) {
	// Create empty clientset (simulates cache sync timeout)
	clientset := fake.NewSimpleClientset()

	baseObs, err := base.NewBaseObserver("test-scheduler")
	require.NoError(t, err)

	obs := &SchedulerObserver{
		BaseObserver: baseObs,
		emitter:      &mockEmitter{events: make([]*domain.ObserverEvent, 0)},
	}

	watcher := NewEventsWatcher(clientset, obs)

	// Use short timeout context to force cache sync timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err = watcher.Run(ctx)

	// Should return error when cache sync fails
	assert.Error(t, err, "Run() should return error when cache sync fails")
	assert.Contains(t, err.Error(), "cache", "Error should mention cache sync failure")
}

// TestEventsWatcher_Run_InformerShutdown verifies informer cleanup
func TestEventsWatcher_Run_InformerShutdown(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	baseObs, err := base.NewBaseObserver("test-scheduler")
	require.NoError(t, err)

	obs := &SchedulerObserver{
		BaseObserver: baseObs,
		emitter:      &mockEmitter{events: make([]*domain.ObserverEvent, 0)},
	}

	watcher := NewEventsWatcher(clientset, obs)

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := watcher.Run(ctx); err != nil {
			t.Logf("watcher.Run() returned error: %v", err)
		}
	}()

	// Give informer time to start
	time.Sleep(100 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait for Run() to return (should not hang)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - Run() returned
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return within 2 seconds after context cancellation")
	}
}
