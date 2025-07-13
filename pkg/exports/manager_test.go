package exports

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManager_NewManager(t *testing.T) {
	config := &ManagerConfig{
		WorkerPoolSize: 4,
		MaxQueueSize:   100,
	}
	
	manager := NewManager(config)
	
	if manager == nil {
		t.Fatal("NewManager returned nil")
	}
	
	if len(manager.plugins) != 0 {
		t.Errorf("Expected 0 plugins, got %d", len(manager.plugins))
	}
	
	if manager.workerPool == nil {
		t.Error("Worker pool not initialized")
	}
	
	if manager.router == nil {
		t.Error("Router not initialized")
	}
	
	if manager.healthMonitor == nil {
		t.Error("Health monitor not initialized")
	}
}

func TestManager_RegisterPlugin(t *testing.T) {
	manager := NewManager(&ManagerConfig{})
	
	// Create a mock plugin
	plugin := &MockExportPlugin{
		name: "test-plugin",
	}
	
	err := manager.RegisterPlugin("test", plugin)
	if err != nil {
		t.Fatalf("Failed to register plugin: %v", err)
	}
	
	// Check if plugin is registered
	if len(manager.plugins) != 1 {
		t.Errorf("Expected 1 plugin, got %d", len(manager.plugins))
	}
	
	if manager.plugins["test"] != plugin {
		t.Error("Plugin not stored correctly")
	}
	
	// Test duplicate registration
	err = manager.RegisterPlugin("test", plugin)
	if err == nil {
		t.Error("Expected error for duplicate registration")
	}
}

func TestManager_UnregisterPlugin(t *testing.T) {
	manager := NewManager(&ManagerConfig{})
	
	plugin := &MockExportPlugin{name: "test-plugin"}
	manager.RegisterPlugin("test", plugin)
	
	err := manager.UnregisterPlugin("test")
	if err != nil {
		t.Fatalf("Failed to unregister plugin: %v", err)
	}
	
	if len(manager.plugins) != 0 {
		t.Errorf("Expected 0 plugins, got %d", len(manager.plugins))
	}
	
	// Test unregistering non-existent plugin
	err = manager.UnregisterPlugin("nonexistent")
	if err == nil {
		t.Error("Expected error for unregistering non-existent plugin")
	}
}

func TestManager_Export(t *testing.T) {
	manager := NewManager(&ManagerConfig{
		WorkerPoolSize: 2,
		MaxQueueSize:   10,
	})
	
	// Register a mock plugin
	plugin := &MockExportPlugin{
		name:           "test-plugin",
		exportDuration: 10 * time.Millisecond,
	}
	manager.RegisterPlugin("test", plugin)
	
	// Add a route
	route := &ExportRoute{
		ID:         "test-route",
		Name:       "Test Route",
		PluginName: "test",
		Enabled:    true,
		Priority:   100,
		Pattern: &RoutePattern{
			DataType: []DataType{DataTypeMetrics},
			Format:   []ExportFormat{FormatJSON},
		},
	}
	manager.router.AddRoute(route)
	
	// Start the manager
	ctx := context.Background()
	err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop(ctx)
	
	// Create export data
	data := ExportData{
		Type:      DataTypeMetrics,
		Format:    FormatJSON,
		Source:    "test",
		Timestamp: time.Now(),
		Content:   map[string]interface{}{"test": "value"},
	}
	
	// Export data
	err = manager.Export(ctx, data)
	if err != nil {
		t.Fatalf("Failed to export data: %v", err)
	}
	
	// Wait for export to complete
	time.Sleep(50 * time.Millisecond)
	
	// Check if plugin received the export
	if plugin.exportCount != 1 {
		t.Errorf("Expected 1 export, got %d", plugin.exportCount)
	}
}

func TestManager_ConfigReload(t *testing.T) {
	// Create temporary config file
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test-config.yaml")
	
	config := map[string]interface{}{
		"plugins": map[string]interface{}{
			"test": map[string]interface{}{
				"enabled": true,
				"config": map[string]interface{}{
					"param1": "value1",
				},
			},
		},
	}
	
	configData, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	
	err = os.WriteFile(configPath, configData, 0644)
	if err != nil {
		t.Fatal(err)
	}
	
	manager := NewManager(&ManagerConfig{})
	
	// Register a mock plugin
	plugin := &MockExportPlugin{name: "test"}
	manager.RegisterPlugin("test", plugin)
	
	// Reload config
	err = manager.ReloadConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to reload config: %v", err)
	}
	
	// Check if plugin was configured
	if !plugin.configured {
		t.Error("Plugin was not configured")
	}
}

func TestManager_GetStats(t *testing.T) {
	manager := NewManager(&ManagerConfig{})
	
	// Register a mock plugin
	plugin := &MockExportPlugin{name: "test"}
	manager.RegisterPlugin("test", plugin)
	
	stats := manager.GetStats()
	
	if stats == nil {
		t.Fatal("GetStats returned nil")
	}
	
	if stats.PluginCount != 1 {
		t.Errorf("Expected 1 plugin, got %d", stats.PluginCount)
	}
	
	if stats.TotalExports != 0 {
		t.Errorf("Expected 0 exports, got %d", stats.TotalExports)
	}
}

func TestManager_Lifecycle(t *testing.T) {
	manager := NewManager(&ManagerConfig{
		WorkerPoolSize: 2,
		MaxQueueSize:   10,
	})
	
	plugin := &MockExportPlugin{name: "test"}
	manager.RegisterPlugin("test", plugin)
	
	ctx := context.Background()
	
	// Test Start
	err := manager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	
	if !plugin.started {
		t.Error("Plugin was not started")
	}
	
	// Test Stop
	err = manager.Stop(ctx)
	if err != nil {
		t.Fatalf("Failed to stop manager: %v", err)
	}
	
	if !plugin.stopped {
		t.Error("Plugin was not stopped")
	}
}

// MockExportPlugin is a mock implementation of ExportPlugin for testing
type MockExportPlugin struct {
	name           string
	started        bool
	stopped        bool
	configured     bool
	exportCount    int
	exportDuration time.Duration
}

func (m *MockExportPlugin) Name() string {
	return m.name
}

func (m *MockExportPlugin) Start(ctx context.Context) error {
	m.started = true
	return nil
}

func (m *MockExportPlugin) Stop(ctx context.Context) error {
	m.stopped = true
	return nil
}

func (m *MockExportPlugin) Configure(config map[string]interface{}) error {
	m.configured = true
	return nil
}

func (m *MockExportPlugin) ValidateConfig() error {
	return nil
}

func (m *MockExportPlugin) GetConfigSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"param1": map[string]interface{}{
				"type": "string",
			},
		},
	}
}

func (m *MockExportPlugin) Export(ctx context.Context, data ExportData) error {
	m.exportCount++
	if m.exportDuration > 0 {
		time.Sleep(m.exportDuration)
	}
	if data.Callback != nil {
		data.Callback(&ExportResult{
			Success:  true,
			Duration: m.exportDuration,
		})
	}
	return nil
}

func (m *MockExportPlugin) SupportedFormats() []ExportFormat {
	return []ExportFormat{FormatJSON, FormatYAML}
}

func (m *MockExportPlugin) SupportedDataTypes() []DataType {
	return []DataType{DataTypeMetrics, DataTypeEvents}
}

func (m *MockExportPlugin) HealthCheck(ctx context.Context) (*HealthStatus, error) {
	return &HealthStatus{
		Healthy:   true,
		LastCheck: time.Now(),
		Message:   "Mock plugin is healthy",
	}, nil
}

func (m *MockExportPlugin) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"exports_total": m.exportCount,
	}
}

// Benchmark tests

func BenchmarkManager_Export(b *testing.B) {
	manager := NewManager(&ManagerConfig{
		WorkerPoolSize: 4,
		MaxQueueSize:   1000,
	})
	
	plugin := &MockExportPlugin{
		name:           "test-plugin",
		exportDuration: 1 * time.Millisecond,
	}
	manager.RegisterPlugin("test", plugin)
	
	route := &ExportRoute{
		ID:         "test-route",
		PluginName: "test",
		Enabled:    true,
		Priority:   100,
		Pattern: &RoutePattern{
			DataType: []DataType{DataTypeMetrics},
		},
	}
	manager.router.AddRoute(route)
	
	ctx := context.Background()
	manager.Start(ctx)
	defer manager.Stop(ctx)
	
	data := ExportData{
		Type:      DataTypeMetrics,
		Format:    FormatJSON,
		Source:    "benchmark",
		Timestamp: time.Now(),
		Content:   map[string]interface{}{"metric": 42},
	}
	
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			manager.Export(ctx, data)
		}
	})
}

func BenchmarkManager_RegisterPlugin(b *testing.B) {
	manager := NewManager(&ManagerConfig{})
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		plugin := &MockExportPlugin{
			name: fmt.Sprintf("plugin-%d", i),
		}
		manager.RegisterPlugin(fmt.Sprintf("plugin-%d", i), plugin)
	}
}

// Test edge cases and error conditions

func TestManager_ExportWithNoRoutes(t *testing.T) {
	manager := NewManager(&ManagerConfig{})
	
	ctx := context.Background()
	manager.Start(ctx)
	defer manager.Stop(ctx)
	
	data := ExportData{
		Type:      DataTypeMetrics,
		Format:    FormatJSON,
		Source:    "test",
		Timestamp: time.Now(),
		Content:   map[string]interface{}{"test": "value"},
	}
	
	// Should not error, but no exports should happen
	err := manager.Export(ctx, data)
	if err != nil {
		t.Errorf("Export with no routes should not error: %v", err)
	}
}

func TestManager_ExportWithFailingPlugin(t *testing.T) {
	manager := NewManager(&ManagerConfig{
		WorkerPoolSize: 1,
		MaxQueueSize:   10,
	})
	
	plugin := &FailingMockPlugin{name: "failing-plugin"}
	manager.RegisterPlugin("failing", plugin)
	
	route := &ExportRoute{
		ID:         "failing-route",
		PluginName: "failing",
		Enabled:    true,
		Priority:   100,
		Pattern: &RoutePattern{
			DataType: []DataType{DataTypeMetrics},
		},
	}
	manager.router.AddRoute(route)
	
	ctx := context.Background()
	manager.Start(ctx)
	defer manager.Stop(ctx)
	
	data := ExportData{
		Type:      DataTypeMetrics,
		Format:    FormatJSON,
		Source:    "test",
		Timestamp: time.Now(),
		Content:   map[string]interface{}{"test": "value"},
	}
	
	// Export should work but plugin will fail internally
	err := manager.Export(ctx, data)
	if err != nil {
		t.Errorf("Export should not return error even if plugin fails: %v", err)
	}
}

// FailingMockPlugin always fails exports
type FailingMockPlugin struct {
	name string
}

func (f *FailingMockPlugin) Name() string { return f.name }
func (f *FailingMockPlugin) Start(ctx context.Context) error { return nil }
func (f *FailingMockPlugin) Stop(ctx context.Context) error { return nil }
func (f *FailingMockPlugin) Configure(config map[string]interface{}) error { return nil }
func (f *FailingMockPlugin) ValidateConfig() error { return nil }
func (f *FailingMockPlugin) GetConfigSchema() map[string]interface{} { return map[string]interface{}{} }

func (f *FailingMockPlugin) Export(ctx context.Context, data ExportData) error {
	if data.Callback != nil {
		data.Callback(&ExportResult{
			Success: false,
			Error:   "Plugin intentionally failed",
		})
	}
	return fmt.Errorf("plugin intentionally failed")
}

func (f *FailingMockPlugin) SupportedFormats() []ExportFormat {
	return []ExportFormat{FormatJSON}
}

func (f *FailingMockPlugin) SupportedDataTypes() []DataType {
	return []DataType{DataTypeMetrics}
}

func (f *FailingMockPlugin) HealthCheck(ctx context.Context) (*HealthStatus, error) {
	return &HealthStatus{
		Healthy:   false,
		LastCheck: time.Now(),
		Message:   "Plugin is failing",
	}, nil
}

func (f *FailingMockPlugin) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"exports_failed": 1,
	}
}

// Integration tests

func TestManager_IntegrationWithRealConfig(t *testing.T) {
	// Create a more realistic configuration
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "integration-config.yaml")
	
	configContent := `
export:
  enabled: true
  worker_pool:
    size: 2
    max_queue_size: 100

plugins:
  test-cli:
    enabled: true
    config:
      output_directory: "` + tempDir + `"
      file_prefix: "test"

routes:
  - id: "test-route"
    name: "Test Route"
    enabled: true
    plugin: "test-cli"
    priority: 100
    pattern:
      data_type: ["metrics"]
`
	
	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatal(err)
	}
	
	manager := NewManager(&ManagerConfig{})
	
	// Register a CLI-like plugin
	plugin := &MockExportPlugin{name: "test-cli"}
	manager.RegisterPlugin("test-cli", plugin)
	
	// Load configuration
	err = manager.ReloadConfig(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}
	
	ctx := context.Background()
	err = manager.Start(ctx)
	if err != nil {
		t.Fatalf("Failed to start manager: %v", err)
	}
	defer manager.Stop(ctx)
	
	// Export some data
	data := ExportData{
		Type:      DataTypeMetrics,
		Format:    FormatJSON,
		Source:    "integration-test",
		Timestamp: time.Now(),
		Content: map[string]interface{}{
			"cpu_usage":    75.5,
			"memory_usage": 60.2,
		},
	}
	
	err = manager.Export(ctx, data)
	if err != nil {
		t.Fatalf("Failed to export data: %v", err)
	}
	
	// Wait for export to complete
	time.Sleep(100 * time.Millisecond)
	
	// Verify export happened
	if plugin.exportCount != 1 {
		t.Errorf("Expected 1 export, got %d", plugin.exportCount)
	}
}

func TestManager_ConcurrentExports(t *testing.T) {
	manager := NewManager(&ManagerConfig{
		WorkerPoolSize: 4,
		MaxQueueSize:   100,
	})
	
	plugin := &MockExportPlugin{
		name:           "concurrent-test",
		exportDuration: 5 * time.Millisecond,
	}
	manager.RegisterPlugin("test", plugin)
	
	route := &ExportRoute{
		ID:         "concurrent-route",
		PluginName: "test",
		Enabled:    true,
		Priority:   100,
		Pattern:    &RoutePattern{},
	}
	manager.router.AddRoute(route)
	
	ctx := context.Background()
	manager.Start(ctx)
	defer manager.Stop(ctx)
	
	// Export multiple items concurrently
	const numExports = 50
	done := make(chan bool, numExports)
	
	for i := 0; i < numExports; i++ {
		go func(id int) {
			data := ExportData{
				Type:      DataTypeMetrics,
				Format:    FormatJSON,
				Source:    fmt.Sprintf("concurrent-%d", id),
				Timestamp: time.Now(),
				Content:   map[string]interface{}{"id": id},
			}
			
			err := manager.Export(ctx, data)
			if err != nil {
				t.Errorf("Export %d failed: %v", id, err)
			}
			done <- true
		}(i)
	}
	
	// Wait for all exports to complete
	for i := 0; i < numExports; i++ {
		<-done
	}
	
	// Wait a bit more for processing
	time.Sleep(100 * time.Millisecond)
	
	// Check that all exports were processed
	if plugin.exportCount != numExports {
		t.Errorf("Expected %d exports, got %d", numExports, plugin.exportCount)
	}
}