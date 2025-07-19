package systemd_test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/systemd"
)

func TestCollector_NewCollector(t *testing.T) {
	config := systemd.DefaultConfig()

	collector, err := systemd.NewCollector(config)
	if runtime.GOOS != "linux" {
		// On non-Linux platforms, we expect it to succeed creation
		// but fail on Start
		if err != nil {
			t.Fatalf("NewCollector should succeed on %s: %v", runtime.GOOS, err)
		}

		// Verify Start fails appropriately
		ctx := context.Background()
		err = collector.Start(ctx)
		if err == nil {
			t.Fatal("Start should fail on non-Linux platforms")
		}
		return
	}

	// Linux-specific tests (may still fail due to D-Bus permissions)
	if err != nil {
		t.Fatalf("NewCollector failed: %v", err)
	}

	if collector == nil {
		t.Fatal("NewCollector returned nil collector")
	}
}

func TestCollector_Health(t *testing.T) {
	config := systemd.DefaultConfig()
	collector, err := systemd.NewCollector(config)
	if err != nil {
		t.Fatalf("NewCollector failed: %v", err)
	}

	health := collector.Health()

	// Before starting, status should be unknown
	if health.Status != systemd.HealthStatusUnknown {
		t.Errorf("Expected status Unknown before start, got %s", health.Status)
	}

	if health.EventsProcessed != 0 {
		t.Errorf("Expected 0 events processed, got %d", health.EventsProcessed)
	}

	if health.DBusConnected {
		t.Error("Expected not connected before start")
	}
}

func TestCollector_Statistics(t *testing.T) {
	config := systemd.DefaultConfig()
	collector, err := systemd.NewCollector(config)
	if err != nil {
		t.Fatalf("NewCollector failed: %v", err)
	}

	stats := collector.Statistics()

	if stats.EventsCollected != 0 {
		t.Errorf("Expected 0 events collected, got %d", stats.EventsCollected)
	}

	if stats.StartTime.After(time.Now()) {
		t.Error("Start time is in the future")
	}

	if stats.ServicesMonitored != 0 {
		t.Errorf("Expected 0 services monitored before start, got %d", stats.ServicesMonitored)
	}
}

func TestCollector_Configure(t *testing.T) {
	config := systemd.DefaultConfig()
	collector, err := systemd.NewCollector(config)
	if err != nil {
		t.Fatalf("NewCollector failed: %v", err)
	}

	// Update configuration
	newConfig := config
	newConfig.EventBufferSize = 2000
	newConfig.WatchAllServices = true

	err = collector.Configure(newConfig)
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}
}

func TestCollector_Lifecycle(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping lifecycle test on non-Linux platform")
	}

	config := systemd.DefaultConfig()
	collector, err := systemd.NewCollector(config)
	if err != nil {
		t.Fatalf("NewCollector failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start collector (may fail due to D-Bus permissions)
	err = collector.Start(ctx)
	if err != nil {
		t.Logf("Start failed (may be due to permissions): %v", err)
		return
	}

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Check health shows it's running
	health := collector.Health()
	if health.Status == systemd.HealthStatusUnknown {
		t.Error("Health status should not be Unknown after start")
	}

	// Stop collector
	err = collector.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name   string
		config systemd.Config
		valid  bool
	}{
		{
			name:   "default config",
			config: systemd.DefaultConfig(),
			valid:  true,
		},
		{
			name: "zero buffer size",
			config: systemd.Config{
				Name:            "test",
				Enabled:         true,
				EventBufferSize: 0, // Should be set to default
			},
			valid: true,
		},
		{
			name: "disabled collector",
			config: systemd.Config{
				Name:    "test",
				Enabled: false,
			},
			valid: true,
		},
		{
			name: "empty unit types",
			config: systemd.Config{
				Name:      "test",
				Enabled:   true,
				UnitTypes: []string{}, // Should default to ["service"]
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := systemd.NewCollector(tt.config)
			if tt.valid && err != nil {
				t.Errorf("Expected valid config, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Error("Expected invalid config, but got no error")
			}
		})
	}
}

func TestDefaultConfigs(t *testing.T) {
	tests := []struct {
		name        string
		configFunc  func() systemd.Config
		expectName  string
		expectTypes []string
	}{
		{
			name:        "default config",
			configFunc:  systemd.DefaultConfig,
			expectName:  "systemd-collector",
			expectTypes: []string{"service"},
		},
		{
			name:        "critical services config",
			configFunc:  systemd.CriticalServicesConfig,
			expectName:  "systemd-critical-collector",
			expectTypes: []string{"service"},
		},
		{
			name:        "all services config",
			configFunc:  systemd.AllServicesConfig,
			expectName:  "systemd-all-collector",
			expectTypes: []string{"service", "socket", "timer"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := tt.configFunc()

			if config.Name != tt.expectName {
				t.Errorf("Expected name %s, got %s", tt.expectName, config.Name)
			}

			if !config.Enabled {
				t.Error("Expected collector to be enabled by default")
			}

			if len(config.UnitTypes) != len(tt.expectTypes) {
				t.Errorf("Expected %d unit types, got %d", len(tt.expectTypes), len(config.UnitTypes))
			}

			for i, expectedType := range tt.expectTypes {
				if i >= len(config.UnitTypes) || config.UnitTypes[i] != expectedType {
					t.Errorf("Expected unit type %s at position %d, got %v", expectedType, i, config.UnitTypes)
				}
			}
		})
	}
}

func TestCriticalServicesConfig(t *testing.T) {
	config := systemd.CriticalServicesConfig()

	// Should have some critical services in the filter
	if len(config.ServiceFilter) == 0 {
		t.Error("Expected critical services config to have service filters")
	}

	// Should include common critical services
	criticalServices := []string{"sshd", "dbus", "systemd-journald"}
	for _, critical := range criticalServices {
		found := false
		for _, filter := range config.ServiceFilter {
			if filter == critical {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected critical service %s to be in filter", critical)
		}
	}
}

func TestAllServicesConfig(t *testing.T) {
	config := systemd.AllServicesConfig()

	if !config.WatchAllServices {
		t.Error("Expected all services config to watch all services")
	}

	if config.EventRateLimit <= systemd.DefaultConfig().EventRateLimit {
		t.Error("Expected all services config to have higher rate limit")
	}

	// Should watch multiple unit types
	expectedTypes := []string{"service", "socket", "timer"}
	if len(config.UnitTypes) < len(expectedTypes) {
		t.Error("Expected all services config to watch multiple unit types")
	}
}
