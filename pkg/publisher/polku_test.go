package publisher

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	// Can't actually connect without a server, but constructor should work
	pub := New(cfg)
	require.NotNil(t, pub)
	assert.Equal(t, "test", pub.clusterID)
	assert.Equal(t, "node-1", pub.nodeName)
}
