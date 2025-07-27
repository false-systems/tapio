package examples

import (
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/integrations/config"
)

// TestSampleIntegration_Basic tests basic integration functionality
func TestSampleIntegration_Basic(t *testing.T) {
	// Create configuration
	cfg := DefaultSampleConfig()
	cfg.Name = "test-integration"
	cfg.Endpoint = "http://localhost:9999"
	cfg.Workers = 2
	cfg.BatchDelay = 100 * time.Millisecond

	// Create integration
	integration, err := NewSampleIntegration(cfg)
	if err != nil {
		t.Fatalf("Failed to create integration: %v", err)
	}

	// Start integration
	if err := integration.Start(); err != nil {
		t.Fatalf("Failed to start integration: %v", err)
	}

	// Let it run for a bit
	time.Sleep(500 * time.Millisecond)

	// Check health
	health := integration.Health()
	if !health.Healthy {
		t.Errorf("Expected integration to be healthy, got unhealthy")
	}
	if health.Status != "running" {
		t.Errorf("Expected status 'running', got '%s'", health.Status)
	}

	// Check statistics
	stats := integration.Statistics()
	if stats.ProcessedCount == 0 {
		t.Errorf("Expected some events to be processed")
	}
	if stats.StartTime.IsZero() {
		t.Errorf("Expected start time to be set")
	}

	// Stop integration
	if err := integration.Stop(); err != nil {
		t.Fatalf("Failed to stop integration: %v", err)
	}
}

// TestSampleIntegration_Configuration tests configuration handling
func TestSampleIntegration_Configuration(t *testing.T) {
	tests := []struct {
		name    string
		config  SampleIntegrationConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: SampleIntegrationConfig{
				BaseConfig: config.DefaultBaseConfig(),
				Endpoint:   "http://localhost:8080",
				BatchSize:  100,
				Workers:    5,
			},
			wantErr: false,
		},
		{
			name: "missing endpoint",
			config: SampleIntegrationConfig{
				BaseConfig: config.DefaultBaseConfig(),
				BatchSize:  100,
				Workers:    5,
			},
			wantErr: true,
		},
		{
			name: "invalid workers count",
			config: SampleIntegrationConfig{
				BaseConfig: config.DefaultBaseConfig(),
				Endpoint:   "http://localhost:8080",
				Workers:    200,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSampleIntegration(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSampleIntegration() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestSampleIntegration_Reload tests configuration reload
func TestSampleIntegration_Reload(t *testing.T) {
	// Create and start integration
	cfg := DefaultSampleConfig()
	cfg.Endpoint = "http://localhost:8080"
	cfg.BatchSize = 50

	integration, err := NewSampleIntegration(cfg)
	if err != nil {
		t.Fatalf("Failed to create integration: %v", err)
	}

	if err := integration.Start(); err != nil {
		t.Fatalf("Failed to start integration: %v", err)
	}
	defer integration.Stop()

	// Reload with new configuration
	newCfg := cfg
	newCfg.BatchSize = 200
	newCfg.BatchDelay = 10 * time.Second

	if err := integration.Reload(newCfg); err != nil {
		t.Fatalf("Failed to reload configuration: %v", err)
	}

	// Verify configuration was updated
	currentCfg := integration.GetConfig().(SampleIntegrationConfig)
	if currentCfg.BatchSize != 200 {
		t.Errorf("Expected batch size 200, got %d", currentCfg.BatchSize)
	}
	if currentCfg.BatchDelay != 10*time.Second {
		t.Errorf("Expected batch delay 10s, got %v", currentCfg.BatchDelay)
	}
}

// TestSampleIntegration_WithBuilder tests integration with config builder
func TestSampleIntegration_WithBuilder(t *testing.T) {
	// Build configuration using builder
	cfgMap, err := config.NewBuilder().
		WithName("builder-test").
		WithType("collector").
		WithEnvironment("test").
		WithRetry(config.RetryConfig{
			Enabled:     true,
			MaxAttempts: 3,
			InitialWait: 50 * time.Millisecond,
		}).
		WithSecurity(config.SecurityConfig{
			TLS: config.TLSConfig{
				Enabled:    true,
				MinVersion: "TLS1.3",
			},
		}).
		WithCustom("endpoint", "http://localhost:8080").
		WithCustom("batch_size", 150).
		WithCustom("workers", 3).
		Build()

	if err != nil {
		t.Fatalf("Failed to build configuration: %v", err)
	}

	// Validate the configuration
	if err := config.Validate(cfgMap); err != nil {
		t.Fatalf("Configuration validation failed: %v", err)
	}

	// Check that builder properly set values
	if cfgMap["name"] != "builder-test" {
		t.Errorf("Expected name 'builder-test', got '%v'", cfgMap["name"])
	}
	if cfgMap["type"] != "collector" {
		t.Errorf("Expected type 'collector', got '%v'", cfgMap["type"])
	}
	if cfgMap["endpoint"] != "http://localhost:8080" {
		t.Errorf("Expected endpoint 'http://localhost:8080', got '%v'", cfgMap["endpoint"])
	}
}

// TestSampleIntegration_HealthStates tests different health states
func TestSampleIntegration_HealthStates(t *testing.T) {
	cfg := DefaultSampleConfig()
	cfg.Endpoint = "http://localhost:8080"

	integration, err := NewSampleIntegration(cfg)
	if err != nil {
		t.Fatalf("Failed to create integration: %v", err)
	}

	// Check health when stopped
	health := integration.Health()
	if health.Healthy {
		t.Errorf("Expected unhealthy when stopped")
	}
	if health.Status != "stopped" {
		t.Errorf("Expected status 'stopped', got '%s'", health.Status)
	}

	// Start and check health
	if err := integration.Start(); err != nil {
		t.Fatalf("Failed to start integration: %v", err)
	}
	defer integration.Stop()

	health = integration.Health()
	if !health.Healthy {
		t.Errorf("Expected healthy when running")
	}
	if health.Status != "running" {
		t.Errorf("Expected status 'running', got '%s'", health.Status)
	}

	// Simulate high error rate
	integration.errorCount = 75
	health = integration.Health()
	if health.Status != "degraded" {
		t.Errorf("Expected status 'degraded' with high errors, got '%s'", health.Status)
	}
}
