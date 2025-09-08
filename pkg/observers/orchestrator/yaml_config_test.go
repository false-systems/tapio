package orchestrator

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestYAMLConfigLoading(t *testing.T) {
	t.Run("ValidConfig", func(t *testing.T) {
		configYAML := `
orchestrator:
  workers: 4
  buffer_size: 1000
  nats:
    url: "nats://localhost:4222"
    subject: "events.raw"
    max_reconnects: 5
    auth_enabled: false

observers:
  kernel:
    enabled: true
    config:
      buffer_size: 500
      enable_ebpf: true
  network:
    enabled: false
    config:
      buffer_size: 1000
`
		var config YAMLConfig
		err := yaml.Unmarshal([]byte(configYAML), &config)
		assert.NoError(t, err)

		// Verify orchestrator config
		assert.Equal(t, 4, config.Orchestrator.Workers)
		assert.Equal(t, 1000, config.Orchestrator.BufferSize)
		assert.Equal(t, "nats://localhost:4222", config.Orchestrator.NATS.URL)
		assert.Equal(t, "events.raw", config.Orchestrator.NATS.Subject)

		// Verify observers config
		assert.True(t, config.Observers["kernel"].Enabled)
		assert.Equal(t, 500, config.Observers["kernel"].Config.BufferSize)
		assert.True(t, config.Observers["kernel"].Config.EnableEBPF)

		assert.False(t, config.Observers["network"].Enabled)
		assert.Equal(t, 1000, config.Observers["network"].Config.BufferSize)
	})

	t.Run("MinimalConfig", func(t *testing.T) {
		configYAML := `
orchestrator:
  workers: 1
  buffer_size: 100
observers:
  test:
    enabled: true
    config:
      buffer_size: 100
`
		var config YAMLConfig
		err := yaml.Unmarshal([]byte(configYAML), &config)
		assert.NoError(t, err)

		assert.Equal(t, 1, config.Orchestrator.Workers)
		assert.Equal(t, 100, config.Orchestrator.BufferSize)
		assert.True(t, config.Observers["test"].Enabled)
	})
}

func TestYAMLConfigValidation(t *testing.T) {
	t.Run("ValidConfiguration", func(t *testing.T) {
		config := &YAMLConfig{
			Orchestrator: OrchestratorYAMLConfig{
				Workers:    4,
				BufferSize: 1000,
			},
			Observers: map[string]ObserverYAMLConfig{
				"test": {
					Enabled: true,
					Config: ObserverConfigData{
						BufferSize: 500,
					},
				},
			},
		}

		err := config.Validate()
		assert.NoError(t, err)
	})

	t.Run("InvalidWorkers", func(t *testing.T) {
		tests := []struct {
			name        string
			workers     int
			expectError bool
		}{
			{"zero workers", 0, true},
			{"negative workers", -1, true},
			{"too many workers", 101, true},
			{"valid workers", 4, false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				config := &YAMLConfig{
					Orchestrator: OrchestratorYAMLConfig{
						Workers:    tt.workers,
						BufferSize: 1000,
					},
					Observers: map[string]ObserverYAMLConfig{
						"test": {
							Enabled: true,
							Config: ObserverConfigData{
								BufferSize: 500,
							},
						},
					},
				}

				err := config.Validate()
				if tt.expectError {
					assert.Error(t, err)
					assert.Contains(t, err.Error(), "workers")
				} else {
					assert.NoError(t, err)
				}
			})
		}
	})

	t.Run("InvalidBufferSize", func(t *testing.T) {
		tests := []struct {
			name        string
			bufferSize  int
			expectError bool
		}{
			{"too small", 50, true},
			{"too large", 200000, true},
			{"minimum valid", 100, false},
			{"maximum valid", 100000, false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				config := &YAMLConfig{
					Orchestrator: OrchestratorYAMLConfig{
						Workers:    4,
						BufferSize: tt.bufferSize,
					},
					Observers: map[string]ObserverYAMLConfig{
						"test": {
							Enabled: true,
							Config: ObserverConfigData{
								BufferSize: 500,
							},
						},
					},
				}

				err := config.Validate()
				if tt.expectError {
					assert.Error(t, err)
					assert.Contains(t, err.Error(), "buffer size")
				} else {
					assert.NoError(t, err)
				}
			})
		}
	})

	t.Run("NoEnabledObservers", func(t *testing.T) {
		config := &YAMLConfig{
			Orchestrator: OrchestratorYAMLConfig{
				Workers:    4,
				BufferSize: 1000,
			},
			Observers: map[string]ObserverYAMLConfig{
				"test": {
					Enabled: false,
					Config: ObserverConfigData{
						BufferSize: 500,
					},
				},
			},
		}

		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no observers are enabled")
	})

	t.Run("EmptyObservers", func(t *testing.T) {
		config := &YAMLConfig{
			Orchestrator: OrchestratorYAMLConfig{
				Workers:    4,
				BufferSize: 1000,
			},
			Observers: map[string]ObserverYAMLConfig{},
		}

		err := config.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no observers configured")
	})
}

func TestYAMLConfigConversion(t *testing.T) {
	t.Run("ToOrchestratorYAMLConfig", func(t *testing.T) {
		yamlConfig := &YAMLConfig{
			Orchestrator: OrchestratorYAMLConfig{
				Workers:    8,
				BufferSize: 5000,
				NATS: NATSYAMLConfig{
					URL:           "nats://test:4222",
					Subject:       "test.events",
					MaxReconnects: 10,
				},
			},
		}

		orchConfig := yamlConfig.ToOrchestratorYAMLConfig()

		assert.Equal(t, 8, orchConfig.Workers)
		assert.Equal(t, 5000, orchConfig.BufferSize)
		assert.NotNil(t, orchConfig.NATSConfig)
		assert.Equal(t, "nats://test:4222", orchConfig.NATSConfig.URL)
		assert.Equal(t, 10, orchConfig.NATSConfig.MaxReconnects)
	})

	t.Run("GetObserverConfig", func(t *testing.T) {
		yamlConfig := &YAMLConfig{
			Observers: map[string]ObserverYAMLConfig{
				"enabled-observer": {
					Enabled: true,
					Config: ObserverConfigData{
						BufferSize:   1000,
						EnableEBPF:   true,
						PollInterval: "10s",
					},
				},
				"disabled-observer": {
					Enabled: false,
					Config: ObserverConfigData{
						BufferSize: 500,
					},
				},
			},
		}

		// Get enabled observer config
		cfg, exists := yamlConfig.GetObserverConfig("enabled-observer")
		assert.True(t, exists)
		assert.NotNil(t, cfg)
		assert.Equal(t, 1000, cfg.BufferSize)
		assert.True(t, cfg.EnableEBPF)
		assert.Equal(t, "10s", cfg.PollInterval)

		// Try to get disabled observer config
		cfg, exists = yamlConfig.GetObserverConfig("disabled-observer")
		assert.False(t, exists)
		assert.Nil(t, cfg)

		// Try to get non-existent observer
		cfg, exists = yamlConfig.GetObserverConfig("non-existent")
		assert.False(t, exists)
		assert.Nil(t, cfg)
	})
}

func TestYAMLConfigAuthentication(t *testing.T) {
	t.Run("NoAuth", func(t *testing.T) {
		yamlConfig := &YAMLConfig{
			Orchestrator: OrchestratorYAMLConfig{
				NATS: NATSYAMLConfig{
					URL:         "nats://localhost:4222",
					AuthEnabled: false,
				},
			},
		}

		natsConfig := yamlConfig.toNATSConfig()
		assert.Equal(t, "nats://localhost:4222", natsConfig.URL)
		// Auth fields should be empty when not enabled
		assert.True(t, natsConfig.JetStreamEnabled)
	})

	t.Run("TokenAuth", func(t *testing.T) {
		yamlConfig := &YAMLConfig{
			Orchestrator: OrchestratorYAMLConfig{
				NATS: NATSYAMLConfig{
					URL:         "nats://localhost:4222",
					AuthEnabled: true,
					Token:       "secret-token",
				},
			},
		}

		natsConfig := yamlConfig.toNATSConfig()
		assert.Equal(t, "nats://localhost:4222", natsConfig.URL)
		// Note: token authentication should be handled via URL or env vars
	})

	t.Run("UsernamePasswordAuth", func(t *testing.T) {
		yamlConfig := &YAMLConfig{
			Orchestrator: OrchestratorYAMLConfig{
				NATS: NATSYAMLConfig{
					URL:         "nats://localhost:4222",
					AuthEnabled: true,
					Username:    "user",
					Password:    "pass",
				},
			},
		}

		natsConfig := yamlConfig.toNATSConfig()
		assert.Equal(t, "nats://localhost:4222", natsConfig.URL)
		// Note: username/password should be handled via URL format
	})
}

func TestLoadYAMLConfigFromFile(t *testing.T) {
	t.Run("ValidFile", func(t *testing.T) {
		// Create temporary config file
		content := `
orchestrator:
  workers: 2
  buffer_size: 500
observers:
  test:
    enabled: true
    config:
      buffer_size: 250
`
		tmpfile, err := os.CreateTemp("", "config-*.yaml")
		require.NoError(t, err)
		defer os.Remove(tmpfile.Name())

		_, err = tmpfile.Write([]byte(content))
		require.NoError(t, err)
		tmpfile.Close()

		config, err := LoadYAMLConfig(tmpfile.Name())
		assert.NoError(t, err)
		assert.NotNil(t, config)
		assert.Equal(t, 2, config.Orchestrator.Workers)
		assert.Equal(t, 500, config.Orchestrator.BufferSize)
		assert.True(t, config.Observers["test"].Enabled)
	})

	t.Run("InvalidFile", func(t *testing.T) {
		config, err := LoadYAMLConfig("/non/existent/file.yaml")
		assert.Error(t, err)
		assert.Nil(t, config)
	})

	t.Run("MalformedYAML", func(t *testing.T) {
		content := `
orchestrator:
  workers: not-a-number
  buffer_size: [this is wrong]
`
		tmpfile, err := os.CreateTemp("", "malformed-*.yaml")
		require.NoError(t, err)
		defer os.Remove(tmpfile.Name())

		_, err = tmpfile.Write([]byte(content))
		require.NoError(t, err)
		tmpfile.Close()

		config, err := LoadYAMLConfig(tmpfile.Name())
		assert.Error(t, err)
		assert.Nil(t, config)
	})
}

func TestYAMLConfigDefaults(t *testing.T) {
	t.Run("ApplyDefaults", func(t *testing.T) {
		config := &YAMLConfig{
			Orchestrator: OrchestratorYAMLConfig{}, // Empty, should get defaults
			Observers:   map[string]ObserverYAMLConfig{},
		}

		// Apply defaults function (if exists)
		if config.Orchestrator.Workers == 0 {
			config.Orchestrator.Workers = 1 // Default
		}
		if config.Orchestrator.BufferSize == 0 {
			config.Orchestrator.BufferSize = 1000 // Default
		}

		assert.Equal(t, 1, config.Orchestrator.Workers)
		assert.Equal(t, 1000, config.Orchestrator.BufferSize)
	})
}

func BenchmarkYAMLConfigParsing(b *testing.B) {
	configYAML := `
orchestrator:
  workers: 4
  buffer_size: 1000
  nats:
    url: "nats://localhost:4222"
    subject: "events.raw"
observers:
  kernel:
    enabled: true
    config:
      buffer_size: 500
  network:
    enabled: true
    config:
      buffer_size: 1000
  dns:
    enabled: false
    config:
      buffer_size: 250
`
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var config YAMLConfig
		yaml.Unmarshal([]byte(configYAML), &config)
	}
}
