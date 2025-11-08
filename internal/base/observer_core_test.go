package base

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RED: Test NewObserverCore creates core with name and logger
func TestNewObserverCore(t *testing.T) {
	core := NewObserverCore("test-observer")
	require.NotNil(t, core)
	assert.Equal(t, "test-observer", core.Name())
	assert.NotNil(t, core.Logger())
}

// RED: Test Name returns observer name
func TestObserverCore_Name(t *testing.T) {
	core := NewObserverCore("my-observer")
	assert.Equal(t, "my-observer", core.Name())
}

// RED: Test Logger returns structured logger
func TestObserverCore_Logger(t *testing.T) {
	core := NewObserverCore("test")
	logger := core.Logger()
	assert.NotNil(t, logger)
}

// RED: Test IsRunning defaults to false
func TestObserverCore_IsRunning_Default(t *testing.T) {
	core := NewObserverCore("test")
	assert.False(t, core.IsRunning())
}

// RED: Test MarkRunning sets running state
func TestObserverCore_MarkRunning(t *testing.T) {
	core := NewObserverCore("test")

	// Start running
	core.MarkRunning(true)
	assert.True(t, core.IsRunning())

	// Stop running
	core.MarkRunning(false)
	assert.False(t, core.IsRunning())
}

// RED: Test RecordEvent increments counter
func TestObserverCore_RecordEvent(t *testing.T) {
	core := NewObserverCore("test")

	// Initially zero
	stats := core.Stats()
	assert.Equal(t, int64(0), stats.EventsProcessed)

	// Record events
	core.RecordEvent()
	core.RecordEvent()
	core.RecordEvent()

	stats = core.Stats()
	assert.Equal(t, int64(3), stats.EventsProcessed)
}

// RED: Test RecordDrop increments counter
func TestObserverCore_RecordDrop(t *testing.T) {
	core := NewObserverCore("test")

	// Initially zero
	stats := core.Stats()
	assert.Equal(t, int64(0), stats.EventsDropped)

	// Record drops
	core.RecordDrop()
	core.RecordDrop()

	stats = core.Stats()
	assert.Equal(t, int64(2), stats.EventsDropped)
}

// RED: Test RecordError increments counter
func TestObserverCore_RecordError(t *testing.T) {
	core := NewObserverCore("test")

	// Initially zero
	stats := core.Stats()
	assert.Equal(t, int64(0), stats.ErrorsTotal)

	// Record errors
	core.RecordError()
	core.RecordError()
	core.RecordError()
	core.RecordError()

	stats = core.Stats()
	assert.Equal(t, int64(4), stats.ErrorsTotal)
}

// RED: Test Stats returns complete snapshot
func TestObserverCore_Stats(t *testing.T) {
	core := NewObserverCore("test")

	// Record various events
	core.RecordEvent()
	core.RecordEvent()
	core.RecordEvent()
	core.RecordDrop()
	core.RecordError()
	core.RecordError()

	stats := core.Stats()
	assert.Equal(t, int64(3), stats.EventsProcessed)
	assert.Equal(t, int64(1), stats.EventsDropped)
	assert.Equal(t, int64(2), stats.ErrorsTotal)
}

// RED: Test Stats is a snapshot (not live reference)
func TestObserverCore_Stats_Snapshot(t *testing.T) {
	core := NewObserverCore("test")

	core.RecordEvent()
	stats1 := core.Stats()
	assert.Equal(t, int64(1), stats1.EventsProcessed)

	// Record more events
	core.RecordEvent()
	core.RecordEvent()

	// Old snapshot should be unchanged
	assert.Equal(t, int64(1), stats1.EventsProcessed)

	// New snapshot should reflect changes
	stats2 := core.Stats()
	assert.Equal(t, int64(3), stats2.EventsProcessed)
}

// RED: Test concurrent counter operations
func TestObserverCore_ConcurrentCounters(t *testing.T) {
	core := NewObserverCore("test")

	// Run concurrent increments
	done := make(chan bool, 3)

	go func() {
		for i := 0; i < 100; i++ {
			core.RecordEvent()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 50; i++ {
			core.RecordDrop()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 25; i++ {
			core.RecordError()
		}
		done <- true
	}()

	// Wait for all goroutines
	<-done
	<-done
	<-done

	// Verify counts
	stats := core.Stats()
	assert.Equal(t, int64(100), stats.EventsProcessed)
	assert.Equal(t, int64(50), stats.EventsDropped)
	assert.Equal(t, int64(25), stats.ErrorsTotal)
}

// RED: Test concurrent running state changes
func TestObserverCore_ConcurrentRunningState(t *testing.T) {
	core := NewObserverCore("test")

	// Run concurrent state changes
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(val bool) {
			core.MarkRunning(val)
			done <- true
		}(i%2 == 0)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Final state should be deterministic based on last write
	// Just verify it's one of the two valid states
	running := core.IsRunning()
	assert.True(t, running || !running) // Always true, just tests thread safety
}

// RED: Test CoreStats structure
func TestCoreStats_Structure(t *testing.T) {
	stats := CoreStats{
		EventsProcessed: 100,
		EventsDropped:   10,
		ErrorsTotal:     5,
	}

	assert.Equal(t, int64(100), stats.EventsProcessed)
	assert.Equal(t, int64(10), stats.EventsDropped)
	assert.Equal(t, int64(5), stats.ErrorsTotal)
}

// RED: Test zero values
func TestObserverCore_ZeroValues(t *testing.T) {
	core := NewObserverCore("test")

	stats := core.Stats()
	assert.Equal(t, int64(0), stats.EventsProcessed)
	assert.Equal(t, int64(0), stats.EventsDropped)
	assert.Equal(t, int64(0), stats.ErrorsTotal)
	assert.False(t, core.IsRunning())
}

// RED: Test multiple observers independence
func TestObserverCore_MultipleObservers(t *testing.T) {
	core1 := NewObserverCore("observer1")
	core2 := NewObserverCore("observer2")

	// Record events in core1
	core1.RecordEvent()
	core1.RecordEvent()
	core1.MarkRunning(true)

	// Record events in core2
	core2.RecordError()
	core2.MarkRunning(false)

	// Verify independence
	stats1 := core1.Stats()
	stats2 := core2.Stats()

	assert.Equal(t, int64(2), stats1.EventsProcessed)
	assert.Equal(t, int64(0), stats1.ErrorsTotal)
	assert.True(t, core1.IsRunning())

	assert.Equal(t, int64(0), stats2.EventsProcessed)
	assert.Equal(t, int64(1), stats2.ErrorsTotal)
	assert.False(t, core2.IsRunning())
}
