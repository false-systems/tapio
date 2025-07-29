package ebpf

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
)

func TestUnifiedCollector(t *testing.T) {
	// Skip on non-Linux
	if runtime.GOOS != "linux" {
		t.Skip("eBPF tests only run on Linux")
	}

	// Skip if not running as root
	if !isRoot() {
		t.Skip("eBPF tests require root privileges")
	}

	config := collectors.DefaultCollectorConfig()
	config.BufferSize = 100

	collector, err := NewUnifiedCollector(config)
	require.NoError(t, err)
	require.NotNil(t, collector)

	t.Run("basic properties", func(t *testing.T) {
		assert.Equal(t, "ebpf-unified", collector.Name())
		assert.True(t, collector.IsHealthy())
	})

	t.Run("start and stop", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Start collector
		err := collector.Start(ctx)
		require.NoError(t, err)

		// Should be healthy
		assert.True(t, collector.IsHealthy())

		// Get event channel
		events := collector.Events()
		assert.NotNil(t, events)

		// Wait for some events
		time.Sleep(100 * time.Millisecond)

		// Stop collector
		err = collector.Stop()
		assert.NoError(t, err)

		// Channel should be closed
		_, ok := <-events
		assert.False(t, ok)
	})
}

func TestUnifiedCollector_Events(t *testing.T) {
	// Skip on non-Linux
	if runtime.GOOS != "linux" {
		t.Skip("eBPF tests only run on Linux")
	}

	// Skip if not running as root
	if !isRoot() {
		t.Skip("eBPF tests require root privileges")
	}

	config := collectors.DefaultCollectorConfig()
	collector, err := NewUnifiedCollector(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Trigger some memory allocations
	go func() {
		for i := 0; i < 10; i++ {
			// Allocate and free memory to trigger events
			data := make([]byte, 1024*1024) // 1MB
			_ = data
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Collect events
	events := make([]collectors.RawEvent, 0)
	timeout := time.After(5 * time.Second)

	for {
		select {
		case event := <-collector.Events():
			events = append(events, event)
			if len(events) >= 5 {
				goto done
			}
		case <-timeout:
			goto done
		}
	}

done:
	// Should have collected some events
	assert.NotEmpty(t, events)

	// Check event properties
	for _, event := range events {
		assert.Equal(t, "ebpf", event.Type)
		assert.NotZero(t, event.Timestamp)
		assert.NotNil(t, event.Data)
		assert.NotNil(t, event.Metadata)

		// Check metadata
		assert.Contains(t, event.Metadata, "event_type")
		assert.Contains(t, event.Metadata, "cpu")
		assert.Contains(t, event.Metadata, "pid")
	}

	// Check metrics
	metrics := collector.GetMetrics()
	assert.Greater(t, metrics.EventsReceived, uint64(0))
	assert.Greater(t, metrics.BytesProcessed, uint64(0))
}

func TestUnifiedCollector_Errors(t *testing.T) {
	config := collectors.DefaultCollectorConfig()
	collector, err := NewUnifiedCollector(config)
	require.NoError(t, err)

	t.Run("double start", func(t *testing.T) {
		// Skip on non-Linux
		if runtime.GOOS != "linux" {
			t.Skip("eBPF tests only run on Linux")
		}

		// Skip if not running as root
		if !isRoot() {
			t.Skip("eBPF tests require root privileges")
		}

		ctx := context.Background()

		err := collector.Start(ctx)
		require.NoError(t, err)
		defer collector.Stop()

		// Second start should fail
		err = collector.Start(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already started")
	})

	t.Run("stop without start", func(t *testing.T) {
		collector := &UnifiedCollector{
			events: make(chan collectors.RawEvent),
		}

		// Should not panic
		err := collector.Stop()
		assert.NoError(t, err)
	})
}

func TestParseUnifiedEvent(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected *unifiedEvent
		wantNil  bool
	}{
		{
			name:    "too small",
			data:    []byte{1, 2, 3},
			wantNil: true,
		},
		{
			name: "valid event",
			data: func() []byte {
				buf := make([]byte, 24)
				// timestamp: 1000
				buf[0] = 0xe8
				buf[1] = 0x03
				// pid: 1234
				buf[8] = 0xd2
				buf[9] = 0x04
				// tid: 5678
				buf[12] = 0x2e
				buf[13] = 0x16
				// cpu: 2
				buf[16] = 0x02
				// type: EVENT_MEMORY
				buf[20] = EventMemory
				// flags: 0
				buf[21] = 0
				// data_len: 0
				buf[22] = 0
				buf[23] = 0
				return buf
			}(),
			expected: &unifiedEvent{
				Timestamp: 1000,
				Pid:       1234,
				Tid:       5678,
				Cpu:       2,
				Type:      EventMemory,
				Flags:     0,
				DataLen:   0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseUnifiedEvent(tt.data)

			if tt.wantNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.expected.Timestamp, result.Timestamp)
				assert.Equal(t, tt.expected.Pid, result.Pid)
				assert.Equal(t, tt.expected.Tid, result.Tid)
				assert.Equal(t, tt.expected.Cpu, result.Cpu)
				assert.Equal(t, tt.expected.Type, result.Type)
				assert.Equal(t, tt.expected.Flags, result.Flags)
			}
		})
	}
}

func TestCreateMetadata(t *testing.T) {
	config := collectors.DefaultCollectorConfig()
	config.Labels = map[string]string{
		"node": "test-node",
		"env":  "test",
	}

	collector := &UnifiedCollector{config: config}

	tests := []struct {
		name     string
		event    *unifiedEvent
		expected map[string]string
	}{
		{
			name: "memory alloc event",
			event: &unifiedEvent{
				Cpu:   1,
				Pid:   1234,
				Tid:   5678,
				Type:  EventMemory,
				Flags: 0,
			},
			expected: map[string]string{
				"cpu":        "1",
				"pid":        "1234",
				"tid":        "5678",
				"event_type": "memory",
				"operation":  "alloc",
				"node":       "test-node",
				"env":        "test",
			},
		},
		{
			name: "memory free event",
			event: &unifiedEvent{
				Cpu:   2,
				Pid:   4321,
				Tid:   8765,
				Type:  EventMemory,
				Flags: 1,
			},
			expected: map[string]string{
				"cpu":        "2",
				"pid":        "4321",
				"tid":        "8765",
				"event_type": "memory",
				"operation":  "free",
				"node":       "test-node",
				"env":        "test",
			},
		},
		{
			name: "network event",
			event: &unifiedEvent{
				Type: EventNetwork,
			},
			expected: map[string]string{
				"event_type": "network",
			},
		},
		{
			name: "oom event",
			event: &unifiedEvent{
				Type: EventOOM,
			},
			expected: map[string]string{
				"event_type": "oom",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			metadata := collector.createMetadata(tt.event)

			for k, v := range tt.expected {
				assert.Equal(t, v, metadata[k])
			}
		})
	}
}

// Helper to check if running as root
func isRoot() bool {
	return os.Geteuid() == 0
}
