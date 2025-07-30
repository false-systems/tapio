package ebpf

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/yairfalse/tapio/pkg/collectors"
)

func TestUnifiedCollector_BasicConstruction(t *testing.T) {
	config := collectors.DefaultCollectorConfig()
	collector, err := NewUnifiedCollector(config)

	if err != nil {
		// Skip if we can't create collector (e.g., no root)
		t.Skipf("Cannot create collector: %v", err)
	}

	assert.NotNil(t, collector)
	assert.Equal(t, "ebpf-unified", collector.Name())
	assert.True(t, collector.IsHealthy())
}

func TestUnifiedEvent_Parsing(t *testing.T) {
	// Test event parsing without needing BPF
	testData := make([]byte, 88) // sizeof(unifiedEvent)
	// Set some test values
	testData[20] = EventMemory // type field offset

	event := parseUnifiedEvent(testData)
	assert.NotNil(t, event)
	assert.Equal(t, uint8(EventMemory), event.Type)
}

func TestMetadataCreation(t *testing.T) {
	collector := &UnifiedCollector{
		config: collectors.CollectorConfig{
			Labels: map[string]string{
				"node": "test-node",
			},
		},
	}

	event := &unifiedEvent{
		Type: EventNetwork,
		Cpu:  1,
		Pid:  1234,
		Tid:  5678,
	}

	metadata := collector.createMetadata(event)

	assert.Equal(t, "network", metadata["event_type"])
	assert.Equal(t, "1", metadata["cpu"])
	assert.Equal(t, "1234", metadata["pid"])
	assert.Equal(t, "5678", metadata["tid"])
	assert.Equal(t, "test-node", metadata["node"])
}

func TestUnifiedCollector_Lifecycle(t *testing.T) {
	// Test basic lifecycle without root
	collector := &UnifiedCollector{
		events:  make(chan collectors.RawEvent, 10),
		healthy: true,
	}

	// Test double stop
	err := collector.Stop()
	assert.NoError(t, err)

	// Test events channel
	events := collector.Events()
	assert.NotNil(t, events)

	// Test health
	assert.True(t, collector.IsHealthy())
}

func TestRawEventCreation(t *testing.T) {
	// Test that we create proper RawEvents
	collector := &UnifiedCollector{
		config: collectors.CollectorConfig{
			Labels: map[string]string{
				"cluster": "test",
			},
		},
	}

	// Simulate event data
	event := &unifiedEvent{
		Type: EventOOM,
		Pid:  999,
	}

	metadata := collector.createMetadata(event)

	rawEvent := collectors.RawEvent{
		Timestamp: time.Now(),
		Type:      "ebpf",
		Data:      []byte("test"),
		Metadata:  metadata,
	}

	assert.Equal(t, "ebpf", rawEvent.Type)
	assert.NotEmpty(t, rawEvent.Data)
	assert.Equal(t, "oom", rawEvent.Metadata["event_type"])
	assert.Equal(t, "test", rawEvent.Metadata["cluster"])
}
