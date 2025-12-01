package test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/internal/runtime"
)

// TestObserver_Creation verifies that we can create a test observer.
func TestObserver_Creation(t *testing.T) {
	observer, err := NewTestObserver("test-observer")

	require.NoError(t, err, "creating test observer should not error")
	require.NotNil(t, observer, "observer should not be nil")
	assert.Equal(t, "test-observer", observer.Name(), "observer name should match")
}

// TestObserver_EmptyName verifies that empty name is rejected.
func TestObserver_EmptyName(t *testing.T) {
	observer, err := NewTestObserver("")

	assert.Error(t, err, "empty name should return error")
	assert.Nil(t, observer, "observer should be nil on error")
	assert.Contains(t, err.Error(), "name is required", "error should mention name requirement")
}

// TestObserver_ProcessorInterface verifies that TestObserver implements EventProcessor.
func TestObserver_ProcessorInterface(t *testing.T) {
	observer, err := NewTestObserver("test")
	require.NoError(t, err)

	var _ runtime.EventProcessor = observer // Compile-time check
}

// TestObserver_Lifecycle verifies the observer lifecycle.
func TestObserver_Lifecycle(t *testing.T) {
	observer, err := NewTestObserver("test")
	require.NoError(t, err)

	ctx := context.Background()
	cfg := runtime.DefaultConfig("test")

	err = observer.Setup(ctx, cfg)
	require.NoError(t, err, "Setup should succeed")

	err = observer.Teardown(ctx)
	require.NoError(t, err, "Teardown should succeed")
}

// TestObserver_Process verifies that Process returns nil (ignores events).
func TestObserver_Process(t *testing.T) {
	observer, err := NewTestObserver("test")
	require.NoError(t, err)

	ctx := context.Background()
	rawEvent := []byte("test event")

	event, err := observer.Process(ctx, rawEvent)
	assert.NoError(t, err, "Process should not error")
	assert.Nil(t, event, "Process should return nil (ignore event)")
}
