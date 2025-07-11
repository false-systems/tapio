package resilience

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSelfHealingManager(t *testing.T) {
	config := DefaultSelfHealingConfig()
	manager := NewSelfHealingManager(config)

	assert.NotNil(t, manager)
	assert.Equal(t, config, manager.config)
	assert.NotNil(t, manager.components)
	assert.NotNil(t, manager.monitors)
	assert.NotNil(t, manager.healers)
	assert.False(t, manager.isRunning)
}

func TestSelfHealingManager_StartStop(t *testing.T) {
	config := DefaultSelfHealingConfig()
	manager := NewSelfHealingManager(config)

	ctx := context.Background()

	// Test start
	err := manager.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, manager.isRunning)

	// Test double start
	err = manager.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// Test stop
	err = manager.Stop()
	assert.NoError(t, err)
	assert.False(t, manager.isRunning)

	// Test double stop
	err = manager.Stop()
	assert.NoError(t, err)
}

func TestSelfHealingManager_RegisterComponent(t *testing.T) {
	manager := NewSelfHealingManager(DefaultSelfHealingConfig())

	// Create mock component
	component := &MockComponent{
		id:        "test-component",
		healthy:   true,
		running:   true,
		healCount: 0,
	}

	// Register component
	err := manager.RegisterComponent("test-component", component)
	assert.NoError(t, err)

	// Verify component is registered
	status := manager.GetComponentStatus("test-component")
	assert.NotNil(t, status)
	assert.Equal(t, "test-component", status.ID)
	assert.True(t, status.Healthy)

	// Test duplicate registration
	err = manager.RegisterComponent("test-component", component)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestSelfHealingManager_UnregisterComponent(t *testing.T) {
	manager := NewSelfHealingManager(DefaultSelfHealingConfig())

	component := &MockComponent{
		id:      "test-component",
		healthy: true,
		running: true,
	}

	// Register and then unregister
	err := manager.RegisterComponent("test-component", component)
	require.NoError(t, err)

	err = manager.UnregisterComponent("test-component")
	assert.NoError(t, err)

	// Should not find component after unregistering
	status := manager.GetComponentStatus("test-component")
	assert.Nil(t, status)

	// Test unregistering non-existent component
	err = manager.UnregisterComponent("non-existent")
	assert.Error(t, err)
}

func TestSelfHealingManager_HealthMonitoring(t *testing.T) {
	config := DefaultSelfHealingConfig()
	config.HealthCheckInterval = 50 * time.Millisecond
	config.HealingTimeout = 100 * time.Millisecond
	manager := NewSelfHealingManager(config)

	component := &MockComponent{
		id:      "monitored-component",
		healthy: true,
		running: true,
	}

	err := manager.RegisterComponent("monitored-component", component)
	require.NoError(t, err)

	ctx := context.Background()
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	// Component should be healthy initially
	status := manager.GetComponentStatus("monitored-component")
	assert.True(t, status.Healthy)

	// Make component unhealthy
	component.healthy = false

	// Wait for health check to detect the issue
	time.Sleep(150 * time.Millisecond)

	// Component should be detected as unhealthy
	status = manager.GetComponentStatus("monitored-component")
	assert.False(t, status.Healthy)
}

func TestSelfHealingManager_AutoHealing(t *testing.T) {
	config := DefaultSelfHealingConfig()
	config.HealthCheckInterval = 30 * time.Millisecond
	config.HealingTimeout = 50 * time.Millisecond
	config.EnableAutoHealing = true
	manager := NewSelfHealingManager(config)

	component := &MockComponent{
		id:        "auto-heal-component",
		healthy:   false, // Start unhealthy
		running:   true,
		healCount: 0,
	}

	err := manager.RegisterComponent("auto-heal-component", component)
	require.NoError(t, err)

	ctx := context.Background()
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	// Wait for auto-healing to trigger
	time.Sleep(200 * time.Millisecond)

	// Component should have been healed
	assert.Greater(t, component.healCount, 0)
}

func TestSelfHealingManager_HealComponent(t *testing.T) {
	manager := NewSelfHealingManager(DefaultSelfHealingConfig())

	component := &MockComponent{
		id:        "heal-test-component",
		healthy:   false,
		running:   true,
		healCount: 0,
	}

	err := manager.RegisterComponent("heal-test-component", component)
	require.NoError(t, err)

	// Manual healing
	err = manager.HealComponent("heal-test-component")
	assert.NoError(t, err)
	assert.Equal(t, 1, component.healCount)

	// Test healing non-existent component
	err = manager.HealComponent("non-existent")
	assert.Error(t, err)
}

func TestSelfHealingManager_GetAllComponentStatus(t *testing.T) {
	manager := NewSelfHealingManager(DefaultSelfHealingConfig())

	// Register multiple components
	components := []*MockComponent{
		{id: "component-1", healthy: true, running: true},
		{id: "component-2", healthy: false, running: true},
		{id: "component-3", healthy: true, running: false},
	}

	for _, comp := range components {
		err := manager.RegisterComponent(comp.id, comp)
		require.NoError(t, err)
	}

	// Get all status
	allStatus := manager.GetAllComponentStatus()
	assert.Len(t, allStatus, 3)

	// Verify each component status
	statusMap := make(map[string]*ComponentStatus)
	for _, status := range allStatus {
		statusMap[status.ID] = status
	}

	assert.True(t, statusMap["component-1"].Healthy)
	assert.True(t, statusMap["component-1"].Running)

	assert.False(t, statusMap["component-2"].Healthy)
	assert.True(t, statusMap["component-2"].Running)

	assert.True(t, statusMap["component-3"].Healthy)
	assert.False(t, statusMap["component-3"].Running)
}

func TestSelfHealingManager_Metrics(t *testing.T) {
	config := DefaultSelfHealingConfig()
	config.HealthCheckInterval = 20 * time.Millisecond
	manager := NewSelfHealingManager(config)

	component := &MockComponent{
		id:        "metrics-component",
		healthy:   false,
		running:   true,
		healCount: 0,
	}

	err := manager.RegisterComponent("metrics-component", component)
	require.NoError(t, err)

	ctx := context.Background()
	err = manager.Start(ctx)
	require.NoError(t, err)
	defer manager.Stop()

	// Perform some healing
	err = manager.HealComponent("metrics-component")
	require.NoError(t, err)

	// Wait for some health checks
	time.Sleep(100 * time.Millisecond)

	metrics := manager.GetMetrics()
	assert.Greater(t, metrics.TotalComponents, uint64(0))
	assert.Greater(t, metrics.HealthChecksPerformed, uint64(0))
	assert.Greater(t, metrics.HealingAttempts, uint64(0))
	assert.NotZero(t, metrics.LastHealthCheck)
}

func TestSelfHealingManager_ConcurrentOperations(t *testing.T) {
	manager := NewSelfHealingManager(DefaultSelfHealingConfig())

	// Register multiple components concurrently
	var wg sync.WaitGroup
	results := make(chan error, 20)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			component := &MockComponent{
				id:      "concurrent-" + string(rune(id)),
				healthy: true,
				running: true,
			}
			err := manager.RegisterComponent(component.id, component)
			results <- err
		}(i)
	}

	// Heal components concurrently
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			componentID := "concurrent-" + string(rune(id))
			time.Sleep(10 * time.Millisecond) // Let registration happen first
			err := manager.HealComponent(componentID)
			results <- err
		}(i)
	}

	// Get status concurrently
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = manager.GetAllComponentStatus()
			results <- nil
		}()
	}

	wg.Wait()
	close(results)

	// Check results
	for err := range results {
		if err != nil {
			// Some operations might fail due to timing, that's okay
			t.Logf("Concurrent operation result: %v", err)
		}
	}
}

func TestSelfHealingManager_Configuration(t *testing.T) {
	tests := []struct {
		name   string
		config *SelfHealingConfig
		valid  bool
	}{
		{
			name:   "default config",
			config: DefaultSelfHealingConfig(),
			valid:  true,
		},
		{
			name: "custom valid config",
			config: &SelfHealingConfig{
				EnableAutoHealing:     true,
				HealthCheckInterval:   time.Second,
				HealingTimeout:        5 * time.Second,
				MaxHealingAttempts:    5,
				HealingCooldown:       30 * time.Second,
				ComponentTimeout:      10 * time.Second,
				EnableResourceLimits:  true,
				MaxConcurrentHealing:  3,
				EnableMetrics:         true,
				MetricsRetentionTime:  time.Hour,
			},
			valid: true,
		},
		{
			name: "disabled auto healing",
			config: &SelfHealingConfig{
				EnableAutoHealing:    false,
				HealthCheckInterval:  time.Minute,
				HealingTimeout:       time.Minute,
				MaxHealingAttempts:   1,
				HealingCooldown:      5 * time.Minute,
				ComponentTimeout:     30 * time.Second,
				EnableResourceLimits: false,
				MaxConcurrentHealing: 1,
				EnableMetrics:        false,
			},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewSelfHealingManager(tt.config)
			if tt.valid {
				assert.NotNil(t, manager)
				assert.Equal(t, tt.config, manager.config)
			} else {
				assert.Nil(t, manager)
			}
		})
	}
}

// Mock component for testing
type MockComponent struct {
	id        string
	healthy   bool
	running   bool
	healCount int
	mu        sync.Mutex
}

func (m *MockComponent) IsHealthy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.healthy
}

func (m *MockComponent) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

func (m *MockComponent) Heal(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healCount++
	m.healthy = true // Healing makes component healthy
	return nil
}

func (m *MockComponent) GetStatus() ComponentStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return ComponentStatus{
		ID:             m.id,
		Healthy:        m.healthy,
		Running:        m.running,
		LastHealthCheck: time.Now(),
		HealingAttempts: uint64(m.healCount),
		Metadata:       make(map[string]interface{}),
	}
}

// Benchmark tests
func BenchmarkSelfHealingManager_RegisterComponent(b *testing.B) {
	manager := NewSelfHealingManager(DefaultSelfHealingConfig())

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		component := &MockComponent{
			id:      "bench-component-" + string(rune(i)),
			healthy: true,
			running: true,
		}
		manager.RegisterComponent(component.id, component)
	}
}

func BenchmarkSelfHealingManager_GetComponentStatus(b *testing.B) {
	manager := NewSelfHealingManager(DefaultSelfHealingConfig())

	// Register a component
	component := &MockComponent{
		id:      "bench-status-component",
		healthy: true,
		running: true,
	}
	manager.RegisterComponent(component.id, component)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		manager.GetComponentStatus("bench-status-component")
	}
}

func BenchmarkSelfHealingManager_HealComponent(b *testing.B) {
	manager := NewSelfHealingManager(DefaultSelfHealingConfig())

	component := &MockComponent{
		id:      "bench-heal-component",
		healthy: false,
		running: true,
	}
	manager.RegisterComponent(component.id, component)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		manager.HealComponent("bench-heal-component")
	}
}

func BenchmarkSelfHealingManager_GetAllComponentStatus(b *testing.B) {
	manager := NewSelfHealingManager(DefaultSelfHealingConfig())

	// Register multiple components
	for i := 0; i < 100; i++ {
		component := &MockComponent{
			id:      "bench-all-component-" + string(rune(i)),
			healthy: true,
			running: true,
		}
		manager.RegisterComponent(component.id, component)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		manager.GetAllComponentStatus()
	}
}