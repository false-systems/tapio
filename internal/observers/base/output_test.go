package base

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultOutputTargets(t *testing.T) {
	targets := DefaultOutputTargets()

	// Default should only have channel enabled
	assert.True(t, targets.Channel, "Channel should be enabled by default")
	assert.False(t, targets.OTEL, "OTEL should be disabled by default")
	assert.False(t, targets.NATS, "NATS should be disabled by default")
	assert.False(t, targets.Stdout, "Stdout should be disabled by default")
}

func TestOutputTargets_Validate(t *testing.T) {
	tests := []struct {
		name    string
		targets OutputTargets
		wantErr bool
	}{
		{
			name: "default targets valid",
			targets: OutputTargets{
				Channel: true,
			},
			wantErr: false,
		},
		{
			name: "otel only valid",
			targets: OutputTargets{
				OTEL: true,
			},
			wantErr: false,
		},
		{
			name: "nats only valid",
			targets: OutputTargets{
				NATS: true,
			},
			wantErr: false,
		},
		{
			name: "multiple outputs valid",
			targets: OutputTargets{
				OTEL:    true,
				NATS:    true,
				Channel: true,
			},
			wantErr: false,
		},
		{
			name: "no outputs invalid",
			targets: OutputTargets{
				OTEL:    false,
				NATS:    false,
				Channel: false,
				Stdout:  false,
			},
			wantErr: true,
		},
		{
			name: "stdout only valid",
			targets: OutputTargets{
				Stdout: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.targets.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestOutputTargets_HasAnyOutput(t *testing.T) {
	tests := []struct {
		name    string
		targets OutputTargets
		want    bool
	}{
		{
			name:    "no outputs",
			targets: OutputTargets{},
			want:    false,
		},
		{
			name: "channel only",
			targets: OutputTargets{
				Channel: true,
			},
			want: true,
		},
		{
			name: "otel only",
			targets: OutputTargets{
				OTEL: true,
			},
			want: true,
		},
		{
			name: "multiple outputs",
			targets: OutputTargets{
				OTEL: true,
				NATS: true,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.targets.HasAnyOutput()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestOTELOutputConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *OTELOutputConfig
		wantErr bool
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
		},
		{
			name: "empty endpoint",
			config: &OTELOutputConfig{
				Endpoint: "",
			},
			wantErr: true,
		},
		{
			name: "valid config",
			config: &OTELOutputConfig{
				Endpoint: "http://localhost:4317",
			},
			wantErr: false,
		},
		{
			name: "valid config with headers",
			config: &OTELOutputConfig{
				Endpoint: "http://localhost:4317",
				Headers: map[string]string{
					"Authorization": "Bearer token",
				},
			},
			wantErr: false,
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

func TestNATSOutputConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *NATSOutputConfig
		wantErr bool
		check   func(t *testing.T, cfg *NATSOutputConfig)
	}{
		{
			name:    "nil config",
			config:  nil,
			wantErr: true,
		},
		{
			name: "empty URL",
			config: &NATSOutputConfig{
				URL: "",
			},
			wantErr: true,
		},
		{
			name: "valid config",
			config: &NATSOutputConfig{
				URL: "nats://localhost:4222",
			},
			wantErr: false,
			check: func(t *testing.T, cfg *NATSOutputConfig) {
				assert.Equal(t, "TAPIO_EVENTS", cfg.StreamName, "Should set default stream name")
				assert.Equal(t, 1000, cfg.MaxPending, "Should set default max pending")
			},
		},
		{
			name: "valid config with custom settings",
			config: &NATSOutputConfig{
				URL:        "nats://localhost:4222",
				StreamName: "CUSTOM_STREAM",
				MaxPending: 5000,
			},
			wantErr: false,
			check: func(t *testing.T, cfg *NATSOutputConfig) {
				assert.Equal(t, "CUSTOM_STREAM", cfg.StreamName)
				assert.Equal(t, 5000, cfg.MaxPending)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.check != nil {
					tt.check(t, tt.config)
				}
			}
		})
	}
}

func TestDomainMetricsCache(t *testing.T) {
	cache := newDomainMetricsCache()

	assert.NotNil(t, cache)
	assert.NotNil(t, cache.counters)
	assert.NotNil(t, cache.gauges)
	assert.Equal(t, 0, len(cache.counters))
	assert.Equal(t, 0, len(cache.gauges))
}
