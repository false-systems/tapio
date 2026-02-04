package intelligence

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/publisher"
)

func TestPolkuServiceConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  PolkuConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: PolkuConfig{
				Publisher: publisher.Config{
					Address:   "polku:50051",
					ClusterID: "prod",
					NodeName:  "node-1",
				},
			},
			wantErr: false,
		},
		{
			name: "missing address",
			config: PolkuConfig{
				Publisher: publisher.Config{
					ClusterID: "prod",
				},
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

func TestPolkuService_Name(t *testing.T) {
	cfg := PolkuConfig{
		Publisher: publisher.Config{
			Address:   "polku:50051",
			ClusterID: "test",
			NodeName:  "node-1",
		},
	}

	svc, err := NewPolkuService(cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, svc.Close()) }()

	assert.Equal(t, "intelligence-polku", svc.Name())
}

func TestPolkuService_IsCritical(t *testing.T) {
	cfg := PolkuConfig{
		Publisher: publisher.Config{
			Address:   "polku:50051",
			ClusterID: "test",
			NodeName:  "node-1",
		},
		Critical: true,
	}

	svc, err := NewPolkuService(cfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, svc.Close()) }()

	assert.True(t, svc.IsCritical())
}

func TestConvertEventType_AllTypes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string // Expected protobuf enum name
	}{
		{"network", "network", "EBPF_TYPE_NETWORK"},
		{"container", "container", "EBPF_TYPE_CONTAINER"},
		{"kernel", "kernel", "EBPF_TYPE_MEMORY"},
		{"storage", "storage", "EBPF_TYPE_STORAGE"},
		{"unknown", "other", "EBPF_TYPE_UNSPECIFIED"},
		{"empty", "", "EBPF_TYPE_UNSPECIFIED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertEventType(tt.input)
			assert.Equal(t, tt.expected, result.String())
		})
	}
}

func TestConvertEvent(t *testing.T) {
	event := &domain.ObserverEvent{
		ID:        "test-123",
		Type:      "network",
		Subtype:   "connection_reset",
		Source:    "network-observer",
		Timestamp: time.Now(),
		NetworkData: &domain.NetworkEventData{
			Protocol: "tcp",
			SrcIP:    "10.0.0.1",
			DstIP:    "10.0.0.2",
			SrcPort:  45678,
			DstPort:  80,
		},
	}

	raw := convertToRawEvent(event, "test-cluster", "node-1")

	require.NotNil(t, raw)
	assert.Equal(t, "test-123", raw.Id)
	assert.Equal(t, "connection_reset", raw.Subtype)
	assert.Equal(t, "test-cluster", raw.ClusterId)
	assert.Equal(t, "node-1", raw.NodeName)

	// Check network data
	require.NotNil(t, raw.GetNetwork())
	assert.Equal(t, "tcp", raw.GetNetwork().Protocol)
	assert.Equal(t, "10.0.0.1", raw.GetNetwork().SrcIp)
	assert.Equal(t, "10.0.0.2", raw.GetNetwork().DstIp)
}
