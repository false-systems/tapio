package network

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

func TestObserverCreation(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name   string
		config *Config
		logger interface{}
		valid  bool
	}{
		{
			name:   "with default config",
			config: nil,
			logger: logger,
			valid:  true,
		},
		{
			name: "with custom config",
			config: &Config{
				BufferSize:    5000,
				EnableIPv4:    true,
				EnableIPv6:    false,
				EnableTCP:     true,
				EnableUDP:     false,
				SamplingRate:  0.5,
				EnableL7Parse: true,
			},
			logger: logger,
			valid:  true,
		},
		{
			name:   "with nil logger",
			config: DefaultConfig(),
			logger: nil,
			valid:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logger = zaptest.NewLogger(t)
			if tt.logger == nil {
				logger = nil
			}
			observer, err := NewObserver("test-observer", tt.config, logger)
			if tt.valid {
				require.NoError(t, err)
				require.NotNil(t, observer)
				assert.Equal(t, "test-observer", observer.Name())
				assert.NotNil(t, observer.config)
				assert.NotNil(t, observer.logger)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestObserverLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)
	observer, err := NewObserver("test-observer", DefaultConfig(), logger)
	require.NoError(t, err)

	// Test Start
	ctx := context.Background()
	err = observer.Start(ctx)
	assert.NoError(t, err)

	// Verify observer is healthy
	health := observer.Health()
	assert.NotNil(t, health)

	// Test Events channel
	events := observer.Events()
	assert.NotNil(t, events)

	// Test Statistics
	stats := observer.Statistics()
	assert.NotNil(t, stats)

	// Test Stop
	err = observer.Stop()
	assert.NoError(t, err)

	// Verify observer health after stop
	health = observer.Health()
	assert.NotNil(t, health)
}

func TestHelperFunctions(t *testing.T) {
	t.Run("GetProtocolName", func(t *testing.T) {
		assert.Equal(t, "TCP", GetProtocolName(ProtocolTCP))
		assert.Equal(t, "UDP", GetProtocolName(ProtocolUDP))
		assert.Equal(t, "ICMP", GetProtocolName(ProtocolICMP))
	})

	t.Run("GetEventTypeName", func(t *testing.T) {
		assert.Equal(t, "connection", GetEventTypeName(EventTypeConnection))
		assert.Equal(t, "dns_query", GetEventTypeName(EventTypeDNSQuery))
	})
}

func TestConfiguration(t *testing.T) {
	config := DefaultConfig()
	assert.NotNil(t, config)
	assert.True(t, config.EnableIPv4)
	assert.True(t, config.EnableTCP)
	assert.True(t, config.EnableL7Parse)
	assert.Contains(t, config.HTTPPorts, 80)
	assert.Equal(t, 53, config.DNSPort)

	err := config.Validate()
	assert.NoError(t, err)
}

func TestEventSending(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultConfig()
	config.BufferSize = 10

	observer, err := NewObserver("test", config, logger)
	require.NoError(t, err)

	// Start observer
	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Create test event with proper validation fields
	event := &domain.CollectorEvent{
		EventID:   "test-1",
		Timestamp: time.Now(),
		Type:      domain.EventTypeTCP,
		Source:    "test",
		Severity:  domain.EventSeverityInfo,
		EventData: domain.EventDataContainer{
			Network: &domain.NetworkData{
				EventType:   "connection",
				Protocol:    "TCP",
				SrcIP:       "10.0.0.1",
				DstIP:       "10.0.0.2",
				SrcPort:     12345,
				DstPort:     80,
				PayloadSize: 1024,
				Direction:   "outbound",
			},
		},
		Metadata: domain.EventMetadata{
			Labels: map[string]string{
				"observer": "test",
				"version":  "1.0",
			},
		},
	}

	// Send event
	observer.SendEvent(event)

	// Check event was received
	select {
	case received := <-observer.Events():
		assert.Equal(t, event.EventID, received.EventID)
	case <-time.After(1 * time.Second):
		t.Fatal("Event not received in time")
	}
}
