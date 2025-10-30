//go:build linux
// +build linux

package node

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"k8s.io/client-go/kubernetes/fake"
)

// TDD Cycle 1: Observer constructor + validation

// TestNewObserver_Success verifies constructor with valid config
func TestNewObserver_Success(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := &mockEmitter{events: make([]*domain.ObserverEvent, 0)}

	cfg := Config{
		Clientset: clientset,
		Emitter:   emitter,
	}

	observer, err := NewObserver("test-observer", cfg)
	require.NoError(t, err)
	require.NotNil(t, observer)
	assert.Equal(t, "test-observer", observer.name)
	assert.NotNil(t, observer.stopCh)
}

// TestNewObserver_MissingClientset verifies error when clientset is nil
func TestNewObserver_MissingClientset(t *testing.T) {
	emitter := &mockEmitter{events: make([]*domain.ObserverEvent, 0)}

	cfg := Config{
		Clientset: nil,
		Emitter:   emitter,
	}

	observer, err := NewObserver("test-observer", cfg)
	require.Error(t, err)
	assert.Nil(t, observer)
	assert.Contains(t, err.Error(), "clientset is required")
}

// TestNewObserver_MissingEmitter verifies error when emitter is nil
func TestNewObserver_MissingEmitter(t *testing.T) {
	clientset := fake.NewSimpleClientset()

	cfg := Config{
		Clientset: clientset,
		Emitter:   nil,
	}

	observer, err := NewObserver("test-observer", cfg)
	require.Error(t, err)
	assert.Nil(t, observer)
	assert.Contains(t, err.Error(), "emitter is required")
}

// TDD Cycle 2: Start/Stop lifecycle + event handlers

// TestObserver_StartStop verifies observer lifecycle
func TestObserver_StartStop(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := &mockEmitter{events: make([]*domain.ObserverEvent, 0)}

	cfg := Config{
		Clientset: clientset,
		Emitter:   emitter,
	}

	observer, err := NewObserver("test-observer", cfg)
	require.NoError(t, err)

	// Observer should be healthy before Start (stopCh initialized)
	assert.True(t, observer.IsHealthy())

	// Start should succeed
	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)

	// Observer should still be healthy after Start
	assert.True(t, observer.IsHealthy())

	// Stop should succeed
	err = observer.Stop()
	require.NoError(t, err)

	// Observer should be unhealthy after Stop (stopCh closed)
	assert.False(t, observer.IsHealthy())
}

// TestObserver_StartRegistersHandlers verifies event handlers are registered
func TestObserver_StartRegistersHandlers(t *testing.T) {
	clientset := fake.NewSimpleClientset()
	emitter := &mockEmitter{events: make([]*domain.ObserverEvent, 0)}

	cfg := Config{
		Clientset: clientset,
		Emitter:   emitter,
	}

	observer, err := NewObserver("test-observer", cfg)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)

	// Verify informer has handlers registered (non-zero handler count)
	// Note: K8s fake client doesn't actually run informers, so this just
	// verifies Start() doesn't panic and completes successfully
	assert.NotNil(t, observer.informer)
}

// Mock emitter for testing
type mockEmitter struct {
	events     []*domain.ObserverEvent
	shouldFail bool
}

func (m *mockEmitter) Emit(ctx context.Context, event *domain.ObserverEvent) error {
	if m.shouldFail {
		return fmt.Errorf("mock emit error")
	}
	m.events = append(m.events, event)
	return nil
}
