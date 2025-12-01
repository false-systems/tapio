package test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/runtime"
)

// TestObserver_Creation verifies that we can create a test observer.
// RED PHASE: This test will FAIL because NewTestObserver doesn't exist yet.
func TestObserver_Creation(t *testing.T) {
	// RED: NewTestObserver doesn't exist yet - test will FAIL
	observer, err := NewTestObserver("test-observer")

	require.NoError(t, err, "creating test observer should not error")
	require.NotNil(t, observer, "observer should not be nil")
	assert.Equal(t, "test-observer", observer.Name(), "observer name should match")
}

// TestObserver_ProcessorInterface verifies that TestObserver implements EventProcessor.
// RED PHASE: This test will FAIL because TestObserver doesn't exist yet.
func TestObserver_ProcessorInterface(t *testing.T) {
	observer, err := NewTestObserver("test")
	require.NoError(t, err)

	// Compile-time check: TestObserver implements EventProcessor
	var _ runtime.EventProcessor = observer // Compile-time check
}

// TestObserver_Lifecycle verifies the observer lifecycle.
// RED PHASE: This test will FAIL because TestObserver doesn't exist yet.
func TestObserver_Lifecycle(t *testing.T) {
	observer, err := NewTestObserver("test")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := runtime.DefaultConfig("test")

	// RED: Setup doesn't exist yet
	err = observer.Setup(ctx, cfg)
	require.NoError(t, err, "Setup should succeed")

	// RED: Teardown doesn't exist yet
	err = observer.Teardown(ctx)
	require.NoError(t, err, "Teardown should succeed")
}
