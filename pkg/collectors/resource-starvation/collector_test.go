package resourcestarvation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestNewCollector(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("with default config", func(t *testing.T) {
		collector, err := NewCollector(nil, logger)
		require.NoError(t, err)
		require.NotNil(t, collector)
		assert.Equal(t, "resource-starvation", collector.config.Name)
		assert.True(t, collector.config.Enabled)
	})

	t.Run("with custom config", func(t *testing.T) {
		config := &Config{
			Name:                  "custom-starvation",
			Enabled:               true,
			StarvationThresholdMS: 200,
			SevereThresholdMS:     1000,
			CriticalThresholdMS:   3000,
			SampleRate:            0.5,
			RingBufferSizeKB:      512,
			MaxEventsPerSec:       500,
			MaxMemoryMB:           256,
			ProcessingTimeout:     time.Second * 10,
			GracefulShutdownSec:   15,
			MaxConsecutiveErrors:  5,
			EventChannelSize:      5000,
			BatchSize:             50,
			BatchTimeout:          500 * time.Millisecond,
			MaxTrackedProcesses:   5000,
			ProcessTrackingTTL:    2 * time.Minute,
		}

		collector, err := NewCollector(config, logger)
		require.NoError(t, err)
		require.NotNil(t, collector)
		assert.Equal(t, "custom-starvation", collector.config.Name)
		assert.Equal(t, 200, collector.config.StarvationThresholdMS)
		assert.Equal(t, 0.5, collector.config.SampleRate)
	})

	t.Run("with invalid config", func(t *testing.T) {
		config := &Config{
			Name:                  "",
			StarvationThresholdMS: -100,
		}

		collector, err := NewCollector(config, logger)
		require.Error(t, err)
		require.Nil(t, collector)
		assert.Contains(t, err.Error(), "invalid config")
	})
}

func TestCollectorLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(nil, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Run("start collector", func(t *testing.T) {
		err := collector.Start(ctx)
		assert.NoError(t, err)
	})

	t.Run("stop collector", func(t *testing.T) {
		err := collector.Stop()
		assert.NoError(t, err)
	})
}

func TestProcessEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(nil, logger)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("process valid event", func(t *testing.T) {
		event := &StarvationEvent{
			Timestamp:  uint64(time.Now().UnixNano()),
			EventType:  uint32(EventSchedWait),
			CPUCore:    0,
			VictimPID:  1234,
			VictimTGID: 1234,
			WaitTimeNS: 150_000_000,
			RunTimeNS:  50_000_000,
		}

		err := collector.ProcessEvent(ctx, event)
		assert.NoError(t, err)
	})

	t.Run("process event with throttling", func(t *testing.T) {
		event := &StarvationEvent{
			Timestamp:   uint64(time.Now().UnixNano()),
			EventType:   uint32(EventCFSThrottle),
			VictimPID:   5678,
			WaitTimeNS:  500_000_000,
			ThrottledNS: 200_000_000,
			NrThrottled: 5,
		}

		err := collector.ProcessEvent(ctx, event)
		assert.NoError(t, err)
	})

	t.Run("process nil event", func(t *testing.T) {
		err := collector.ProcessEvent(ctx, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot process nil event")
	})
}

func TestDetectStarvationPattern(t *testing.T) {
	logger := zaptest.NewLogger(t)
	collector, err := NewCollector(nil, logger)
	require.NoError(t, err)

	tests := []struct {
		name     string
		event    *StarvationEvent
		expected string
	}{
		{
			name: "throttle pattern",
			event: &StarvationEvent{
				EventType:   uint32(EventCFSThrottle),
				ThrottledNS: 100_000_000,
			},
			expected: PatternThrottle,
		},
		{
			name: "sustained pattern",
			event: &StarvationEvent{
				EventType:  uint32(EventSchedWait),
				WaitTimeNS: 2_000_000_000,
			},
			expected: PatternSustained,
		},
		{
			name: "cache thrash pattern",
			event: &StarvationEvent{
				EventType: uint32(EventCoreMigrate),
			},
			expected: PatternCacheThrash,
		},
		{
			name: "noisy neighbor pattern",
			event: &StarvationEvent{
				EventType: uint32(EventNoisyNeighbor),
			},
			expected: PatternNoisyNeighbor,
		},
		{
			name: "burst pattern default",
			event: &StarvationEvent{
				EventType:  uint32(EventSchedWait),
				WaitTimeNS: 50_000_000,
			},
			expected: PatternBurst,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := collector.detectStarvationPattern(tt.event)
			assert.Equal(t, tt.expected, pattern)
		})
	}
}

func BenchmarkProcessEvent(b *testing.B) {
	logger := zaptest.NewLogger(b)
	collector, err := NewCollector(nil, logger)
	require.NoError(b, err)

	ctx := context.Background()
	event := &StarvationEvent{
		Timestamp:  uint64(time.Now().UnixNano()),
		EventType:  uint32(EventSchedWait),
		VictimPID:  1234,
		WaitTimeNS: 100_000_000,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := collector.ProcessEvent(ctx, event)
		if err != nil {
			b.Fatal(err)
		}
	}
}
