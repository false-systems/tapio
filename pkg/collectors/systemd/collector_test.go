package systemd

import (
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/systemd/core"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.Name != "systemd-collector" {
		t.Errorf("Expected name systemd-collector, got %s", config.Name)
	}

	if !config.Enabled {
		t.Error("Expected collector to be enabled by default")
	}

	if config.EventBufferSize != 1000 {
		t.Errorf("Expected buffer size 1000, got %d", config.EventBufferSize)
	}

	// Check that default settings are sensible
	if !config.WatchServiceStates {
		t.Error("Expected WatchServiceStates to be true by default")
	}

	if !config.WatchServiceFailures {
		t.Error("Expected WatchServiceFailures to be true by default")
	}

	// Check unit types include service
	foundService := false
	for _, unitType := range config.UnitTypes {
		if unitType == "service" {
			foundService = true
			break
		}
	}
	if !foundService {
		t.Error("Expected 'service' to be in default unit types")
	}
}

func TestNewCollector(t *testing.T) {
	config := core.Config{
		Name:                 "test-collector",
		Enabled:              true,
		EventBufferSize:      100,
		WatchServiceStates:   true,
		WatchServiceFailures: true,
		UnitTypes:            []string{"service"},
	}

	collector, err := NewCollector(config)
	// Note: This may fail on non-Linux systems or without D-Bus
	// In a real test environment, we'd use dependency injection for mocking
	if err != nil {
		t.Skipf("Skipping test - collector creation failed (expected on non-Linux): %v", err)
		return
	}

	if collector == nil {
		t.Error("Expected collector to be created")
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  core.Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: core.Config{
				Name:            "test",
				Enabled:         true,
				EventBufferSize: 100,
				PollInterval:    time.Second,
			},
			wantErr: false,
		},
		{
			name: "zero buffer size gets default",
			config: core.Config{
				Name:            "test",
				EventBufferSize: 0,
			},
			wantErr: false,
		},
		{
			name: "zero poll interval gets default",
			config: core.Config{
				Name:         "test",
				PollInterval: 0,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				// Check defaults were applied
				if tt.config.EventBufferSize == 0 {
					t.Error("Expected default EventBufferSize to be set")
				}
				if tt.config.PollInterval == 0 {
					t.Error("Expected default PollInterval to be set")
				}
			}
		})
	}
}

func TestConfigServiceFiltering(t *testing.T) {
	config := core.Config{
		Name:           "test",
		Enabled:        true,
		ServiceFilter:  []string{"nginx.service", "mysql.service"},
		ServiceExclude: []string{"*-test", "*-debug"},
	}

	if err := config.Validate(); err != nil {
		t.Fatalf("Failed to validate config: %v", err)
	}

	// Check that filtering options are preserved
	if len(config.ServiceFilter) != 2 {
		t.Errorf("Expected 2 service filters, got %d", len(config.ServiceFilter))
	}

	if len(config.ServiceExclude) != 2 {
		t.Errorf("Expected 2 service excludes, got %d", len(config.ServiceExclude))
	}
}

func TestConfigUnitTypes(t *testing.T) {
	config := core.Config{
		Name:      "test",
		Enabled:   true,
		UnitTypes: []string{"service", "timer", "socket"},
	}

	if err := config.Validate(); err != nil {
		t.Fatalf("Failed to validate config: %v", err)
	}

	if len(config.UnitTypes) != 3 {
		t.Errorf("Expected 3 unit types, got %d", len(config.UnitTypes))
	}

	// Verify specific unit types
	expectedTypes := map[string]bool{"service": true, "timer": true, "socket": true}
	for _, unitType := range config.UnitTypes {
		if !expectedTypes[unitType] {
			t.Errorf("Unexpected unit type: %s", unitType)
		}
	}
}
