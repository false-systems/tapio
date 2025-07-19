package k8s_test

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/k8s"
)

func TestCollector_NewCollector(t *testing.T) {
	config := k8s.DefaultConfig()

	collector, err := k8s.NewCollector(config)
	if err != nil {
		t.Fatalf("NewCollector failed: %v", err)
	}

	if collector == nil {
		t.Fatal("NewCollector returned nil collector")
	}
}

func TestCollector_Health(t *testing.T) {
	config := k8s.DefaultConfig()
	collector, err := k8s.NewCollector(config)
	if err != nil {
		t.Fatalf("NewCollector failed: %v", err)
	}

	health := collector.Health()

	// Before starting, status should be unknown
	if health.Status != k8s.HealthStatusUnknown {
		t.Errorf("Expected status Unknown before start, got %s", health.Status)
	}

	if health.EventsProcessed != 0 {
		t.Errorf("Expected 0 events processed, got %d", health.EventsProcessed)
	}

	if health.Connected {
		t.Error("Expected not connected before start")
	}
}

func TestCollector_Statistics(t *testing.T) {
	config := k8s.DefaultConfig()
	collector, err := k8s.NewCollector(config)
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

	if stats.WatchersActive != 0 {
		t.Errorf("Expected 0 active watchers before start, got %d", stats.WatchersActive)
	}
}

func TestCollector_Configure(t *testing.T) {
	config := k8s.DefaultConfig()
	collector, err := k8s.NewCollector(config)
	if err != nil {
		t.Fatalf("NewCollector failed: %v", err)
	}

	// Update configuration
	newConfig := config
	newConfig.EventBufferSize = 2000
	newConfig.WatchPods = false
	newConfig.WatchNodes = false

	err = collector.Configure(newConfig)
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}
}

func TestCollector_Lifecycle(t *testing.T) {
	config := k8s.DefaultConfig()
	// For tests, we won't have a valid kubeconfig
	config.KubeConfig = "/non/existent/path"
	config.InCluster = false

	collector, err := k8s.NewCollector(config)
	if err != nil {
		t.Fatalf("NewCollector failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start collector - should fail due to invalid kubeconfig
	err = collector.Start(ctx)
	if err == nil {
		t.Fatal("Expected Start to fail with invalid kubeconfig")
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name   string
		config k8s.Config
		valid  bool
	}{
		{
			name:   "default config",
			config: k8s.DefaultConfig(),
			valid:  true,
		},
		{
			name: "zero buffer size",
			config: k8s.Config{
				Name:            "test",
				Enabled:         true,
				EventBufferSize: 0, // Should be set to default
			},
			valid: true,
		},
		{
			name: "disabled collector",
			config: k8s.Config{
				Name:    "test",
				Enabled: false,
			},
			valid: true,
		},
		{
			name: "no resources to watch",
			config: k8s.Config{
				Name:             "test",
				Enabled:          true,
				WatchPods:        false,
				WatchNodes:       false,
				WatchServices:    false,
				WatchDeployments: false,
				WatchEvents:      false,
				WatchConfigMaps:  false,
				WatchSecrets:     false,
			},
			valid: true, // Should default to watching pods and events
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := k8s.NewCollector(tt.config)
			if tt.valid && err != nil {
				t.Errorf("Expected valid config, got error: %v", err)
			}
			if !tt.valid && err == nil {
				t.Error("Expected invalid config, but got no error")
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := k8s.DefaultConfig()

	if config.Name != "k8s-collector" {
		t.Errorf("Expected name 'k8s-collector', got %s", config.Name)
	}

	if !config.Enabled {
		t.Error("Expected collector to be enabled by default")
	}

	if config.EventBufferSize != 1000 {
		t.Errorf("Expected buffer size 1000, got %d", config.EventBufferSize)
	}

	// Should have some resources enabled by default
	resourceCount := 0
	if config.WatchPods {
		resourceCount++
	}
	if config.WatchNodes {
		resourceCount++
	}
	if config.WatchServices {
		resourceCount++
	}
	if config.WatchDeployments {
		resourceCount++
	}
	if config.WatchEvents {
		resourceCount++
	}

	if resourceCount == 0 {
		t.Error("Expected at least one resource type to be watched by default")
	}

	// Secrets and ConfigMaps should be disabled by default
	if config.WatchSecrets {
		t.Error("Expected secrets watching to be disabled by default")
	}
	if config.WatchConfigMaps {
		t.Error("Expected configmaps watching to be disabled by default")
	}
}
