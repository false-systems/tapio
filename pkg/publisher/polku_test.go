package publisher

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tapiopb "github.com/yairfalse/proto/gen/go/tapio/v1"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Address:   "polku:50051",
				ClusterID: "prod",
				NodeName:  "node-1",
			},
			wantErr: false,
		},
		{
			name: "missing address",
			config: Config{
				ClusterID: "prod",
				NodeName:  "node-1",
			},
			wantErr: true,
		},
		{
			name: "missing cluster",
			config: Config{
				Address:  "polku:50051",
				NodeName: "node-1",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfig_Defaults(t *testing.T) {
	cfg := Config{
		Address:   "polku:50051",
		ClusterID: "prod",
		NodeName:  "node-1",
	}

	cfg.ApplyDefaults()

	assert.Equal(t, 100, cfg.BatchSize)
	assert.Equal(t, 100*time.Millisecond, cfg.FlushInterval)
	assert.Equal(t, 1000, cfg.BufferSize)
}

func TestNewPublisher(t *testing.T) {
	cfg := Config{
		Address:   "localhost:50051",
		ClusterID: "test",
		NodeName:  "node-1",
	}
	cfg.ApplyDefaults()

	pub := New(cfg)
	require.NotNil(t, pub)
	assert.Equal(t, "test", pub.clusterID)
	assert.Equal(t, "node-1", pub.nodeName)
}

func TestConfig_ReconnectDefaults(t *testing.T) {
	cfg := Config{
		Address:   "polku:50051",
		ClusterID: "prod",
	}

	cfg.ApplyDefaults()

	assert.Equal(t, 1*time.Second, cfg.ReconnectInitial)
	assert.Equal(t, 30*time.Second, cfg.ReconnectMax)
}

func TestPublisher_IsConnected(t *testing.T) {
	cfg := Config{
		Address:   "localhost:50051",
		ClusterID: "test",
		NodeName:  "node-1",
	}
	cfg.ApplyDefaults()

	pub := New(cfg)
	assert.False(t, pub.IsConnected(), "should not be connected before Connect")
}

func TestPublisher_Publish_BufferFull(t *testing.T) {
	cfg := Config{
		Address:    "localhost:50051",
		ClusterID:  "test",
		NodeName:   "node-1",
		BufferSize: 2,
		BatchSize:  10, // High so no auto-flush
	}
	cfg.ApplyDefaults()

	pub := New(cfg)

	// Fill buffer
	event := &tapiopb.RawEbpfEvent{Id: "test-1"}
	require.NoError(t, pub.Publish(event))
	require.NoError(t, pub.Publish(event))

	// Third should fail
	err := pub.Publish(event)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "buffer full")
}

func TestPublisher_Throttle_Initial(t *testing.T) {
	cfg := Config{
		Address:   "localhost:50051",
		ClusterID: "test",
		NodeName:  "node-1",
	}
	cfg.ApplyDefaults()

	pub := New(cfg)
	// Initial throttle should be 0 (no throttling configured yet)
	assert.Equal(t, 0, pub.Throttle())
}

func TestPublisher_Close_NotConnected(t *testing.T) {
	cfg := Config{
		Address:   "localhost:50051",
		ClusterID: "test",
		NodeName:  "node-1",
	}
	cfg.ApplyDefaults()

	pub := New(cfg)
	// Close without Connect should not panic
	err := pub.Close()
	assert.NoError(t, err)
}
