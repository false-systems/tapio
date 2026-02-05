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

func TestBuildTransportCredentials_Insecure(t *testing.T) {
	cfg := Config{
		Address:   "localhost:50051",
		ClusterID: "test",
		NodeName:  "node-1",
	}
	cfg.ApplyDefaults()

	pub := New(cfg)
	creds, err := pub.buildTransportCredentials()

	assert.NoError(t, err)
	assert.NotNil(t, creds)
	// Insecure credentials should have "insecure" protocol
	info := creds.Info()
	assert.Equal(t, "insecure", info.SecurityProtocol)
}

func TestBuildTransportCredentials_TLS_CertError(t *testing.T) {
	cfg := Config{
		Address:   "localhost:50051",
		ClusterID: "test",
		NodeName:  "node-1",
		TLSCert:   "/nonexistent/cert.pem",
		TLSKey:    "/nonexistent/key.pem",
	}
	cfg.ApplyDefaults()

	pub := New(cfg)
	_, err := pub.buildTransportCredentials()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "load client cert")
}

func TestPublisher_Flush_NotConnected(t *testing.T) {
	cfg := Config{
		Address:   "localhost:50051",
		ClusterID: "test",
		NodeName:  "node-1",
	}
	cfg.ApplyDefaults()

	pub := New(cfg)
	// Not connected - flush should return nil (no error, just skip)
	err := pub.flush()
	assert.NoError(t, err)
}

func TestPublisher_Flush_EmptyBuffer(t *testing.T) {
	cfg := Config{
		Address:   "localhost:50051",
		ClusterID: "test",
		NodeName:  "node-1",
	}
	cfg.ApplyDefaults()

	pub := New(cfg)
	pub.connected.Store(true) // Fake connected state

	// Empty buffer - flush should return nil
	err := pub.flush()
	assert.NoError(t, err)
}

func TestPublisher_Flush_NoStream(t *testing.T) {
	cfg := Config{
		Address:   "localhost:50051",
		ClusterID: "test",
		NodeName:  "node-1",
	}
	cfg.ApplyDefaults()

	pub := New(cfg)
	pub.connected.Store(true) // Fake connected state

	// Add an event to buffer
	pub.buffer = append(pub.buffer, &tapiopb.RawEbpfEvent{Id: "test"})

	// No stream set - should fail
	err := pub.flush()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream not available")
}

func TestPublisher_Publish_ThrottleDrop(t *testing.T) {
	cfg := Config{
		Address:    "localhost:50051",
		ClusterID:  "test",
		NodeName:   "node-1",
		BufferSize: 100,
		BatchSize:  100,
	}
	cfg.ApplyDefaults()

	pub := New(cfg)
	pub.throttle.Store(50) // 50% throttle

	// With throttle at 50%, events with ID[0] >= 50 should be dropped
	// Create event that will be dropped based on deterministic sampling
	event := &tapiopb.RawEbpfEvent{Id: "z"} // 'z' = 122 % 100 = 22, which is < 50, so NOT dropped

	// Event with ID starting with high ASCII should be dropped
	eventDropped := &tapiopb.RawEbpfEvent{Id: string([]byte{100})} // 100 % 100 = 0, < 50, NOT dropped

	// Event with ID starting with ASCII 60 should be dropped (60 >= 50)
	eventDropped2 := &tapiopb.RawEbpfEvent{Id: string([]byte{60})} // 60 >= 50, dropped

	err := pub.Publish(event)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(pub.buffer))

	err = pub.Publish(eventDropped)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(pub.buffer))

	// This one should be dropped
	err = pub.Publish(eventDropped2)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(pub.buffer)) // Still 2 because event was dropped
}

func TestPublisher_SignalReconnect(t *testing.T) {
	cfg := Config{
		Address:   "localhost:50051",
		ClusterID: "test",
		NodeName:  "node-1",
	}
	cfg.ApplyDefaults()

	pub := New(cfg)
	pub.connected.Store(true)

	pub.signalReconnect()

	assert.False(t, pub.IsConnected())

	// Channel should have a signal
	select {
	case <-pub.reconnectCh:
		// Expected
	default:
		t.Fatal("expected reconnect signal")
	}
}

func TestPublisher_SignalReconnect_Multiple(t *testing.T) {
	cfg := Config{
		Address:   "localhost:50051",
		ClusterID: "test",
		NodeName:  "node-1",
	}
	cfg.ApplyDefaults()

	pub := New(cfg)
	pub.connected.Store(true)

	// Multiple signals should not block (channel has buffer of 1)
	pub.signalReconnect()
	pub.signalReconnect()
	pub.signalReconnect()

	assert.False(t, pub.IsConnected())
}
