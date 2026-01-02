package k8scontext

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTombstoneCache_Basic(t *testing.T) {
	tc := NewTombstoneCache(30 * time.Second)

	pod := &PodMeta{
		UID:       "uid-1",
		Name:      "nginx",
		Namespace: "default",
		PodIP:     "10.0.1.5",
	}

	tc.Add(pod)

	// Should be found in tombstone
	got, ok := tc.GetByIP("10.0.1.5")
	require.True(t, ok)
	assert.Equal(t, "nginx", got.Name)
	assert.True(t, got.Terminating)
}

func TestTombstoneCache_Expiry(t *testing.T) {
	tc := NewTombstoneCache(50 * time.Millisecond)

	pod := &PodMeta{
		UID:   "uid-1",
		PodIP: "10.0.1.5",
	}

	tc.Add(pod)

	// Should exist initially
	_, ok := tc.GetByIP("10.0.1.5")
	require.True(t, ok)

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	// Should be gone
	_, ok = tc.GetByIP("10.0.1.5")
	assert.False(t, ok)
}

func TestTombstoneCache_Cleanup(t *testing.T) {
	tc := NewTombstoneCache(50 * time.Millisecond)

	tc.Add(&PodMeta{UID: "uid-1", PodIP: "10.0.1.1"})
	tc.Add(&PodMeta{UID: "uid-2", PodIP: "10.0.1.2"})

	assert.Equal(t, 2, tc.Count())

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	// Cleanup
	tc.Cleanup()

	assert.Equal(t, 0, tc.Count())
}

func TestTombstoneCache_CleanupLoop(t *testing.T) {
	tc := NewTombstoneCache(50 * time.Millisecond)

	tc.Add(&PodMeta{UID: "uid-1", PodIP: "10.0.1.1"})

	ctx, cancel := context.WithCancel(context.Background())

	// Start cleanup loop with short interval
	go tc.StartCleanupLoop(ctx, 30*time.Millisecond)

	// Wait for cleanup to run
	time.Sleep(150 * time.Millisecond)

	assert.Equal(t, 0, tc.Count())

	cancel()
}
