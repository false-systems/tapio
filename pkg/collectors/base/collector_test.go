package base

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
)

func TestBaseCollector(t *testing.T) {
	t.Run("initialization", func(t *testing.T) {
		bc := NewBaseCollector("test-collector", 5*time.Minute)
		
		assert.Equal(t, "test-collector", bc.GetName())
		assert.Equal(t, int64(0), bc.GetEventCount())
		assert.Equal(t, int64(0), bc.GetErrorCount())
		assert.Equal(t, int64(0), bc.GetDroppedCount())
		assert.True(t, bc.GetUptime() >= 0)
	})

	t.Run("record events", func(t *testing.T) {
		bc := NewBaseCollector("test-collector", 5*time.Minute)
		
		bc.RecordEvent()
		bc.RecordEvent()
		bc.RecordEvent()
		
		assert.Equal(t, int64(3), bc.GetEventCount())
	})

	t.Run("record errors", func(t *testing.T) {
		bc := NewBaseCollector("test-collector", 5*time.Minute)
		
		bc.RecordError(errors.New("test error 1"))
		bc.RecordError(errors.New("test error 2"))
		
		assert.Equal(t, int64(2), bc.GetErrorCount())
	})

	t.Run("record drops", func(t *testing.T) {
		bc := NewBaseCollector("test-collector", 5*time.Minute)
		
		bc.RecordDrop()
		bc.RecordDrop()
		bc.RecordDrop()
		bc.RecordDrop()
		
		assert.Equal(t, int64(4), bc.GetDroppedCount())
	})

	t.Run("statistics", func(t *testing.T) {
		bc := NewBaseCollector("test-collector", 5*time.Minute)
		
		bc.RecordEvent()
		bc.RecordEvent()
		bc.RecordError(errors.New("test"))
		bc.RecordDrop()
		
		stats := bc.Statistics()
		
		assert.Equal(t, int64(2), stats.EventsProcessed)
		assert.Equal(t, int64(1), stats.ErrorCount)
		assert.Equal(t, "1", stats.CustomMetrics["events_dropped"])
		assert.True(t, stats.Uptime > 0)
		assert.False(t, stats.LastEventTime.IsZero())
	})

	t.Run("health when healthy", func(t *testing.T) {
		bc := NewBaseCollector("test-collector", 5*time.Minute)
		bc.RecordEvent()
		
		health := bc.Health()
		
		assert.Equal(t, domain.HealthHealthy, health.Status)
		assert.Contains(t, health.Message, "operating normally")
	})

	t.Run("health when unhealthy", func(t *testing.T) {
		bc := NewBaseCollector("test-collector", 5*time.Minute)
		bc.SetHealthy(false)
		bc.RecordError(errors.New("critical error"))
		
		health := bc.Health()
		
		assert.Equal(t, domain.HealthUnhealthy, health.Status)
		assert.Contains(t, health.Message, "unhealthy")
	})

	t.Run("health degraded on high error rate", func(t *testing.T) {
		bc := NewBaseCollector("test-collector", 5*time.Minute)
		
		// Create >10% error rate
		for i := 0; i < 10; i++ {
			bc.RecordEvent()
		}
		bc.RecordError(errors.New("error1"))
		bc.RecordError(errors.New("error2"))
		
		health := bc.Health()
		
		assert.Equal(t, domain.HealthDegraded, health.Status)
		assert.Contains(t, health.Message, "High error rate")
	})

	t.Run("health degraded on stale events", func(t *testing.T) {
		bc := NewBaseCollector("test-collector", 100*time.Millisecond) // Short timeout
		bc.RecordEvent()
		
		// Wait for timeout
		time.Sleep(150 * time.Millisecond)
		
		health := bc.Health()
		
		assert.Equal(t, domain.HealthDegraded, health.Status)
		assert.Contains(t, health.Message, "No events received")
	})
}

func TestEventChannelManager(t *testing.T) {
	t.Run("send and receive events", func(t *testing.T) {
		ecm := NewEventChannelManager(10, "test", nil)
		defer ecm.Close()
		
		event := &domain.CollectorEvent{
			EventID: "test-1",
			Type:    domain.EventTypeKernelProcess,
		}
		
		sent := ecm.SendEvent(event)
		assert.True(t, sent)
		assert.Equal(t, int64(1), ecm.GetSentCount())
		
		// Receive event
		select {
		case received := <-ecm.GetChannel():
			assert.Equal(t, "test-1", received.EventID)
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Did not receive event")
		}
	})

	t.Run("drop events when channel full", func(t *testing.T) {
		ecm := NewEventChannelManager(2, "test", nil) // Small channel
		defer ecm.Close()
		
		// Fill channel
		ecm.SendEvent(&domain.CollectorEvent{EventID: "1"})
		ecm.SendEvent(&domain.CollectorEvent{EventID: "2"})
		
		// This should be dropped
		sent := ecm.SendEvent(&domain.CollectorEvent{EventID: "3"})
		
		assert.False(t, sent)
		assert.Equal(t, int64(1), ecm.GetDroppedCount())
		assert.Equal(t, int64(2), ecm.GetSentCount())
	})

	t.Run("channel utilization", func(t *testing.T) {
		ecm := NewEventChannelManager(10, "test", nil)
		defer ecm.Close()
		
		// Add 5 events to channel with capacity 10
		for i := 0; i < 5; i++ {
			ecm.SendEvent(&domain.CollectorEvent{EventID: fmt.Sprintf("%d", i)})
		}
		
		utilization := ecm.GetChannelUtilization()
		assert.Equal(t, 50.0, utilization)
	})
}

func TestLifecycleManager(t *testing.T) {
	t.Run("start and stop goroutines", func(t *testing.T) {
		lm := NewLifecycleManager(nil, nil)
		
		done := make(chan bool)
		lm.Start("test-goroutine", func() {
			<-lm.StopChannel()
			done <- true
		})
		
		assert.Equal(t, int32(1), lm.GetRunningGoroutines())
		
		err := lm.Stop(1 * time.Second)
		require.NoError(t, err)
		
		select {
		case <-done:
			// Success
		case <-time.After(2 * time.Second):
			t.Fatal("Goroutine did not stop")
		}
		
		assert.Equal(t, int32(0), lm.GetRunningGoroutines())
	})

	t.Run("shutdown timeout", func(t *testing.T) {
		lm := NewLifecycleManager(nil, nil)
		
		// Start goroutine that won't stop
		lm.Start("stuck-goroutine", func() {
			time.Sleep(10 * time.Second)
		})
		
		err := lm.Stop(100 * time.Millisecond)
		assert.Equal(t, ErrShutdownTimeout, err)
	})

	t.Run("context cancellation", func(t *testing.T) {
		lm := NewLifecycleManager(nil, nil)
		
		contextCancelled := false
		lm.Start("context-aware", func() {
			<-lm.Context().Done()
			contextCancelled = true
		})
		
		err := lm.Stop(1 * time.Second)
		require.NoError(t, err)
		
		time.Sleep(100 * time.Millisecond)
		assert.True(t, contextCancelled)
	})
}

func BenchmarkBaseCollector(b *testing.B) {
	bc := NewBaseCollector("bench", 5*time.Minute)
	
	b.Run("RecordEvent", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			bc.RecordEvent()
		}
	})
	
	b.Run("RecordError", func(b *testing.B) {
		err := errors.New("bench error")
		for i := 0; i < b.N; i++ {
			bc.RecordError(err)
		}
	})
	
	b.Run("Statistics", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = bc.Statistics()
		}
	})
	
	b.Run("Health", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = bc.Health()
		}
	})
}

func BenchmarkEventChannelManager(b *testing.B) {
	ecm := NewEventChannelManager(1000, "bench", nil)
	defer ecm.Close()
	
	event := &domain.CollectorEvent{
		EventID: "bench",
		Type:    domain.EventTypeKernelProcess,
	}
	
	b.Run("SendEvent", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			ecm.SendEvent(event)
		}
	})
}