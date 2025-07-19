package ebpf_test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/ebpf"
)

func TestCollector_NewCollector(t *testing.T) {
	config := ebpf.DefaultConfig()

	collector, err := ebpf.NewCollector(config)
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

	// Linux-specific tests
	if err != nil {
		t.Fatalf("NewCollector failed: %v", err)
	}

	if collector == nil {
		t.Fatal("NewCollector returned nil collector")
	}
}

func TestCollector_Health(t *testing.T) {
	config := ebpf.DefaultConfig()
	collector, err := ebpf.NewCollector(config)
	if err != nil {
		t.Fatalf("NewCollector failed: %v", err)
	}

	health := collector.Health()

	// Before starting, status should be unknown
	if health.Status != ebpf.HealthStatusUnknown {
		t.Errorf("Expected status Unknown before start, got %s", health.Status)
	}

	if health.EventsProcessed != 0 {
		t.Errorf("Expected 0 events processed, got %d", health.EventsProcessed)
	}
}

func TestCollector_Statistics(t *testing.T) {
	config := ebpf.DefaultConfig()
	collector, err := ebpf.NewCollector(config)
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
}

func TestCollector_Configure(t *testing.T) {
	config := ebpf.DefaultConfig()
	collector, err := ebpf.NewCollector(config)
	if err != nil {
		t.Fatalf("NewCollector failed: %v", err)
	}

	// Update configuration
	newConfig := config
	newConfig.EventBufferSize = 2000
	newConfig.EnableNetwork = false

	err = collector.Configure(newConfig)
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}
}

func TestCollector_Lifecycle(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping lifecycle test on non-Linux platform")
	}

	config := ebpf.DefaultConfig()
	collector, err := ebpf.NewCollector(config)
	if err != nil {
		t.Fatalf("NewCollector failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start collector
	err = collector.Start(ctx)
	if err != nil {
		// May fail due to permissions, which is okay for tests
		t.Logf("Start failed (may be due to permissions): %v", err)
		return
	}

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Check health shows it's running
	health := collector.Health()
	if health.Status == ebpf.HealthStatusUnknown {
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
		config ebpf.Config
		valid  bool
	}{
		{
			name:   "default config",
			config: ebpf.DefaultConfig(),
			valid:  true,
		},
		{
			name: "zero buffer size",
			config: ebpf.Config{
				Name:            "test",
				Enabled:         true,
				EventBufferSize: 0, // Should be set to default
			},
			valid: true,
		},
		{
			name: "disabled collector",
			config: ebpf.Config{
				Name:    "test",
				Enabled: false,
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ebpf.NewCollector(tt.config)
			if tt.valid && err != nil {
				t.Errorf("Expected valid config, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Error("Expected invalid config, but got no error")
			}
		})
	}
}
