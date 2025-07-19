package internal

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/journald/core"
)

func TestNewCollector(t *testing.T) {
	tests := []struct {
		name    string
		config  core.Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: core.Config{
				Name:            "test-collector",
				Enabled:         true,
				EventBufferSize: 100,
				FollowMode:      false,
				ReadTimeout:     10 * time.Second,
			},
			wantErr: false,
		},
		{
			name: "valid config with defaults",
			config: core.Config{
				Name:    "test-collector",
				Enabled: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector, err := NewCollector(tt.config)

			// On non-Linux platforms, platform initialization will fail
			if runtime.GOOS != "linux" {
				if err == nil {
					t.Error("Expected error on non-Linux platform")
				}
				t.Skipf("Skipping test on non-Linux platform (%s)", runtime.GOOS)
				return
			}

			if (err != nil) != tt.wantErr {
				t.Errorf("NewCollector() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && collector == nil {
				t.Error("NewCollector() returned nil collector")
			}
		})
	}
}

func TestCollectorLifecycle(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("Skipping test on non-Linux platform (%s)", runtime.GOOS)
		return
	}

	config := core.Config{
		Name:            "test-collector",
		Enabled:         true,
		EventBufferSize: 10,
		FollowMode:      false,
		ReadTimeout:     1 * time.Second,
	}

	collector, err := NewCollector(config)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Test initial state
	health := collector.Health()
	if health.Status != core.HealthStatusUnknown {
		t.Errorf("Expected initial health status to be unknown, got %v", health.Status)
	}

	// Test double start protection
	ctx := context.Background()

	// First start should work (or fail gracefully on non-Linux)
	_ = collector.Start(ctx)
	err2 := collector.Start(ctx)

	if err2 == nil {
		t.Error("Expected second Start() call to fail")
	}

	// Test stop
	err = collector.Stop()
	if err != nil {
		t.Errorf("Stop() failed: %v", err)
	}

	// Test double stop protection
	err = collector.Stop()
	if err != nil {
		t.Errorf("Second Stop() call failed: %v", err)
	}
}

func TestCollectorConfiguration(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("Skipping test on non-Linux platform (%s)", runtime.GOOS)
		return
	}

	config := core.Config{
		Name:    "test-collector",
		Enabled: true,
	}

	collector, err := NewCollector(config)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Test valid configuration update
	newConfig := core.Config{
		Name:            "updated-collector",
		Enabled:         false,
		EventBufferSize: 200,
	}

	err = collector.Configure(newConfig)
	if err != nil {
		t.Errorf("Configure() failed: %v", err)
	}
}

func TestCollectorStatistics(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("Skipping test on non-Linux platform (%s)", runtime.GOOS)
		return
	}

	config := core.Config{
		Name:    "test-collector",
		Enabled: true,
	}

	collector, err := NewCollector(config)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	stats := collector.Statistics()

	// Verify initial statistics
	if stats.StartTime.IsZero() {
		t.Error("Expected StartTime to be set")
	}

	if stats.Custom == nil {
		t.Error("Expected Custom metrics to be initialized")
	}

	// Verify that statistics contain expected fields
	expectedFields := []string{
		"uptime_seconds",
		"events_per_second",
		"journal_open",
		"current_cursor",
		"boot_id",
		"machine_id",
	}

	for _, field := range expectedFields {
		if _, exists := stats.Custom[field]; !exists {
			t.Errorf("Expected custom metric %s to exist", field)
		}
	}
}

func TestEventChannel(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("Skipping test on non-Linux platform (%s)", runtime.GOOS)
		return
	}

	config := core.Config{
		Name:            "test-collector",
		Enabled:         true,
		EventBufferSize: 10,
	}

	collector, err := NewCollector(config)
	if err != nil {
		t.Fatalf("Failed to create collector: %v", err)
	}

	// Test that Events() returns a channel
	eventChan := collector.Events()
	if eventChan == nil {
		t.Error("Events() returned nil channel")
	}

	// Channel should be readable (though it may be empty)
	select {
	case <-eventChan:
		// Got an event, that's fine
	default:
		// No events, that's also fine for this test
	}
}
