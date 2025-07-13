package systemd

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
)

// TestCollectorCreation tests basic collector creation
func TestCollectorCreation(t *testing.T) {
	config := collectors.DefaultCollectorConfig("test-systemd", "systemd")
	config.Extra["monitor_all_services"] = false
	config.Extra["service_whitelist"] = []string{"docker.service", "containerd.service"}
	
	collector, err := NewCollector(config)
	if err != nil {
		// Skip if D-Bus is not available (CI environment)
		t.Skip("D-Bus not available:", err)
	}
	defer collector.Stop()
	
	// Verify basic properties
	if collector.Name() != "test-systemd" {
		t.Errorf("Expected name 'test-systemd', got '%s'", collector.Name())
	}
	
	if collector.Type() != "systemd" {
		t.Errorf("Expected type 'systemd', got '%s'", collector.Type())
	}
	
	if !collector.IsEnabled() {
		t.Error("Collector should be enabled by default")
	}
}

// TestCollectorLifecycle tests start/stop lifecycle
func TestCollectorLifecycle(t *testing.T) {
	config := collectors.DefaultCollectorConfig("test-systemd", "systemd")
	config.EventBufferSize = 100
	
	collector, err := NewCollector(config)
	if err != nil {
		t.Skip("D-Bus not available:", err)
	}
	defer collector.Stop()
	
	// Start collector
	ctx := context.Background()
	err = collector.Start(ctx)
	if err != nil {
		t.Fatal("Failed to start collector:", err)
	}
	
	// Let it run briefly
	time.Sleep(100 * time.Millisecond)
	
	// Check health
	health := collector.Health()
	if health.Status != collectors.HealthStatusHealthy &&
		health.Status != collectors.HealthStatusDegraded {
		t.Errorf("Expected healthy or degraded status, got %s: %s", 
			health.Status, health.Message)
	}
	
	// Stop collector
	err = collector.Stop()
	if err != nil {
		t.Error("Failed to stop collector:", err)
	}
	
	// Verify stopped
	health = collector.Health()
	if health.Status != collectors.HealthStatusStopped {
		t.Errorf("Expected stopped status, got %s", health.Status)
	}
}

// TestDBusConnection tests D-Bus connection handling
func TestDBusConnection(t *testing.T) {
	config := DefaultDBusConfig()
	config.SignalBufferSize = 100
	
	dbus, err := NewDBusConnection(config)
	if err != nil {
		t.Skip("D-Bus not available:", err)
	}
	defer dbus.Close()
	
	// Check connection
	conn, err := dbus.GetConnection()
	if err != nil {
		t.Fatal("Failed to get connection:", err)
	}
	
	if conn == nil {
		t.Fatal("Connection is nil")
	}
	
	// Subscribe to signals
	err = dbus.SubscribeToSystemdSignals()
	if err != nil {
		t.Error("Failed to subscribe to signals:", err)
	}
	
	// Check stats
	stats := dbus.GetStats()
	if !stats.IsConnected {
		t.Error("D-Bus should be connected")
	}
}

// TestServiceMonitor tests service monitoring
func TestServiceMonitor(t *testing.T) {
	dbusConfig := DefaultDBusConfig()
	dbus, err := NewDBusConnection(dbusConfig)
	if err != nil {
		t.Skip("D-Bus not available:", err)
	}
	defer dbus.Close()
	
	config := DefaultServiceMonitorConfig()
	config.EventBufferSize = 100
	config.MonitorAllServices = false
	config.ServiceWhitelist = []string{"systemd-"}
	
	monitor, err := NewServiceMonitor(dbus, config)
	if err != nil {
		t.Fatal("Failed to create service monitor:", err)
	}
	
	err = monitor.Start()
	if err != nil {
		t.Fatal("Failed to start monitor:", err)
	}
	defer monitor.Stop()
	
	// Let it discover services
	time.Sleep(500 * time.Millisecond)
	
	// Check stats
	stats := monitor.GetStats()
	if stats.ServicesMonitored == 0 {
		t.Log("No services monitored - this is expected in minimal environments")
	}
}

// TestRestartPatternDetection tests pattern detection logic
func TestRestartPatternDetection(t *testing.T) {
	config := RestartPatternConfig{
		Window:           5 * time.Minute,
		Threshold:        3,
		AnomalyDetection: true,
		BaselinePeriod:   24 * time.Hour,
		AnomalyStdDevs:   2.0,
	}
	
	detector := NewRestartPatternDetector(config)
	
	// Test crash loop detection
	now := time.Now()
	restarts := []time.Time{
		now.Add(-4 * time.Minute),
		now.Add(-3 * time.Minute),
		now.Add(-2 * time.Minute),
		now.Add(-1 * time.Minute),
		now,
	}
	
	pattern := detector.DetectPattern("test.service", restarts)
	if pattern == nil {
		t.Fatal("Should detect pattern")
	}
	
	if pattern.Type != PatternCrashLoop && pattern.Type != PatternRapidRestart {
		t.Errorf("Expected crash loop or rapid restart pattern, got %s", pattern.Type)
	}
	
	if pattern.Occurrences != 5 {
		t.Errorf("Expected 5 occurrences, got %d", pattern.Occurrences)
	}
}

// TestDependencyGraph tests dependency tracking
func TestDependencyGraph(t *testing.T) {
	graph := NewServiceDependencyGraph()
	
	// Add dependencies
	graph.AddDependency("web.service", "database.service")
	graph.AddDependency("web.service", "cache.service")
	graph.AddDependency("api.service", "database.service")
	graph.AddDependency("frontend.service", "api.service")
	
	// Test direct dependencies
	deps := graph.GetDependencies("web.service")
	if len(deps) != 2 {
		t.Errorf("Expected 2 dependencies, got %d", len(deps))
	}
	
	// Test dependents
	dependents := graph.GetDependents("database.service")
	if len(dependents) != 2 {
		t.Errorf("Expected 2 dependents, got %d", len(dependents))
	}
	
	// Test transitive dependents
	transitive := graph.GetTransitiveDependents("database.service", 3)
	if len(transitive) < 2 {
		t.Errorf("Expected at least 2 transitive dependents, got %d", len(transitive))
	}
}

// BenchmarkEventProcessing benchmarks event processing performance
func BenchmarkEventProcessing(b *testing.B) {
	config := collectors.DefaultCollectorConfig("bench-systemd", "systemd")
	config.EventBufferSize = 10000
	
	collector, err := NewCollector(config)
	if err != nil {
		b.Skip("D-Bus not available:", err)
	}
	defer collector.Stop()
	
	ctx := context.Background()
	err = collector.Start(ctx)
	if err != nil {
		b.Fatal("Failed to start collector:", err)
	}
	
	// Create test events
	events := make([]*ServiceEvent, b.N)
	for i := 0; i < b.N; i++ {
		events[i] = &ServiceEvent{
			Timestamp:    time.Now(),
			Service:      "test.service",
			EventType:    ServiceStarted,
			Severity:     collectors.SeverityLow,
			OldState:     "inactive",
			NewState:     "active",
			RestartCount: i % 10,
		}
	}
	
	b.ResetTimer()
	
	// Process events
	for i := 0; i < b.N; i++ {
		collector.convertServiceEvent(events[i])
	}
}