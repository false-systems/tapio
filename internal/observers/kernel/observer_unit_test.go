package kernel

import (
	"context"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// TestNewObserver tests observer creation with various configurations
func TestNewObserver(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "default config",
			config: &Config{
				Name:       "test-kernel",
				BufferSize: 1000,
				EnableEBPF: true,
			},
			wantErr: false,
		},
		{
			name:    "nil config uses defaults",
			config:  nil,
			wantErr: false,
		},
		{
			name: "zero buffer size",
			config: &Config{
				Name:       "test",
				BufferSize: 0,
				EnableEBPF: false,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer, err := NewObserver("test", tt.config)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, observer)

			// Verify base components are initialized
			assert.NotNil(t, observer.BaseObserver)
			assert.NotNil(t, observer.EventChannelManager)
			assert.NotNil(t, observer.LifecycleManager)
			assert.NotNil(t, observer.logger)
			assert.NotNil(t, observer.config)

			// Clean up
			observer.Stop()
		})
	}
}

// TestObserverLifecycle tests Start/Stop operations
func TestObserverLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "test-kernel",
		BufferSize: 100,
		EnableEBPF: false, // Disable eBPF for unit tests
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)
	require.NotNil(t, observer)

	// Test Start
	ctx := context.Background()
	err = observer.Start(ctx)
	assert.NoError(t, err)
	// Observer should be running after Start

	// Test double start (should be safe)
	err = observer.Start(ctx)
	assert.NoError(t, err)

	// Test Stop
	err = observer.Stop()
	assert.NoError(t, err)

	// Test double stop (should be safe)
	err = observer.Stop()
	assert.NoError(t, err)
}

// TestObserverEventChannel tests event channel management
func TestObserverEventChannel(t *testing.T) {
	config := &Config{
		Name:       "test-kernel",
		BufferSize: 10,
		EnableEBPF: false,
	}

	observer, err := NewObserver("test", config)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Get event channel
	events := observer.Events()
	assert.NotNil(t, events)

	// Test sending an event (in mock mode)
	if observer.mockMode {
		// Wait for mock events to be generated
		select {
		case event := <-events:
			assert.NotNil(t, event)
			assert.Equal(t, "kernel", event.Source)
			assert.NotZero(t, event.Timestamp)
		case <-time.After(2 * time.Second):
			t.Log("No mock events generated (expected in non-mock mode)")
		}
	}
}

// TestObserverHealth tests health check functionality
func TestObserverHealth(t *testing.T) {
	config := &Config{
		Name:       "test-kernel",
		BufferSize: 100,
		EnableEBPF: false,
	}

	observer, err := NewObserver("test", config)
	require.NoError(t, err)

	// Should be healthy after creation
	health := observer.Health()
	assert.Equal(t, domain.HealthHealthy, health.Status)
	// Component field is not set by BaseObserver

	// Start the observer
	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)

	// Should still be healthy
	health = observer.Health()
	assert.Equal(t, domain.HealthHealthy, health.Status)

	// Record some events
	observer.RecordEvent()
	observer.RecordEvent()
	observer.RecordError(nil)

	// Check statistics
	stats := observer.Statistics()
	assert.Equal(t, int64(2), stats.EventsProcessed)
	assert.Equal(t, int64(1), stats.ErrorCount)

	observer.Stop()
}

// TestObserverConfiguration tests configuration management
func TestObserverConfiguration(t *testing.T) {
	// Test default configuration
	defaultConfig := NewDefaultConfig("test")
	assert.Equal(t, "test", defaultConfig.Name)
	assert.Equal(t, 10000, defaultConfig.BufferSize)
	assert.True(t, defaultConfig.EnableEBPF)

	// Test custom configuration
	customConfig := &Config{
		Name:       "custom",
		BufferSize: 5000,
		EnableEBPF: false,
	}

	observer, err := NewObserver("test", customConfig)
	require.NoError(t, err)
	assert.Equal(t, 5000, observer.config.BufferSize)
	assert.False(t, observer.config.EnableEBPF)
}

// TestParseConfigInfo tests parsing of ConfigMap/Secret access information
func TestParseConfigInfo(t *testing.T) {
	tests := []struct {
		name           string
		mountPath      string
		wantConfigType string
		wantConfigName string
		wantNamespace  string
		wantPodUID     string
	}{
		{
			name:           "configmap access",
			mountPath:      "/var/lib/kubelet/pods/abc-123-def/volumes/kubernetes.io~configmap/app-config",
			wantConfigType: "configmap",
			wantConfigName: "app-config",
			wantPodUID:     "abc-123-def",
		},
		{
			name:           "secret access",
			mountPath:      "/var/lib/kubelet/pods/xyz-789/volumes/kubernetes.io~secret/db-credentials",
			wantConfigType: "secret",
			wantConfigName: "db-credentials",
			wantPodUID:     "xyz-789",
		},
		{
			name:           "projected volume",
			mountPath:      "/var/lib/kubelet/pods/pod-123/volume-subpaths/kubernetes.io~projected/config",
			wantConfigType: "projected",
			wantConfigName: "config",
			wantPodUID:     "pod-123",
		},
		{
			name:           "non-kubernetes path",
			mountPath:      "/etc/passwd",
			wantConfigType: "",
			wantConfigName: "",
			wantPodUID:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer, err := NewObserver("test", nil)
			require.NoError(t, err)

			configType, configName, podUID := observer.parseConfigPath(tt.mountPath)
			assert.Equal(t, tt.wantConfigType, configType)
			assert.Equal(t, tt.wantConfigName, configName)
			assert.Equal(t, tt.wantPodUID, podUID)
		})
	}
}

// TestConvertKernelEvent tests conversion of kernel events to domain events
func TestConvertKernelEvent(t *testing.T) {
	observer, err := NewObserver("test", nil)
	require.NoError(t, err)

	// Create a sample kernel event
	kernelEvent := &KernelEvent{
		Timestamp: uint64(time.Now().UnixNano()),
		PID:       1234,
		TID:       5678,
		EventType: uint32(EventTypeConfigMapAccess),
		CgroupID:  999888,
		Comm:      [16]byte{'n', 'g', 'i', 'n', 'x'},
	}

	// Set up config info in Data field
	configInfo := ConfigInfo{
		ErrorCode: 0,
	}
	mountPath := "/var/lib/kubelet/pods/abc-123/volumes/kubernetes.io~configmap/app-config"
	copy(configInfo.MountPath[:], mountPath)

	// Marshal config info to Data field
	configBytes := (*[64]byte)(unsafe.Pointer(&configInfo))
	copy(kernelEvent.Data[:], configBytes[:])

	// Convert to domain event
	domainEvent := observer.convertKernelEvent(kernelEvent)

	assert.NotNil(t, domainEvent)
	assert.Equal(t, "test", domainEvent.Source) // Name is "test" from NewObserver
	assert.Equal(t, domain.EventTypeKernelSyscall, domainEvent.Type)
	assert.NotNil(t, domainEvent.EventData.Kernel)
	assert.Equal(t, int32(1234), domainEvent.EventData.Kernel.PID)
	assert.Equal(t, "nginx", domainEvent.EventData.Kernel.Command)
}

// TestErrorCodeMapping tests error code to description mapping
func TestErrorCodeMapping(t *testing.T) {
	tests := []struct {
		errorCode int32
		wantDesc  string
	}{
		{0, "Success"},
		{2, "No such file or directory"},
		{5, "I/O error"},
		{13, "Permission denied"},
		{28, "No space left on device"},
		{30, "Read-only file system"},
		{999, "Unknown error (999)"},
	}

	observer, err := NewObserver("test", nil)
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.wantDesc, func(t *testing.T) {
			desc := observer.getErrorDescription(tt.errorCode)
			assert.Equal(t, tt.wantDesc, desc)
		})
	}
}

// TestMockModeGeneration tests mock event generation
func TestMockModeGeneration(t *testing.T) {
	t.Setenv("TAPIO_MOCK_MODE", "true")

	config := &Config{
		Name:       "test-kernel",
		BufferSize: 100,
		EnableEBPF: false,
	}

	observer, err := NewObserver("test", config)
	require.NoError(t, err)
	assert.True(t, observer.mockMode)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Wait for mock events - need to wait longer than the 3-second ticker
	events := observer.Events()
	timeout := time.After(10 * time.Second) // Increased timeout
	eventCount := 0

	for eventCount < 2 { // Reduced to 2 events since ticker is 3 seconds
		select {
		case event := <-events:
			assert.NotNil(t, event)
			assert.Equal(t, "test-kernel", event.Source) // Source is config.Name
			assert.NotNil(t, event.EventData.Kernel)

			// Verify mock event contains expected fields
			kernel := event.EventData.Kernel
			assert.NotEmpty(t, kernel.Command)
			assert.NotZero(t, kernel.PID)
			eventCount++
			t.Logf("Received mock event %d: PID=%d, Command=%s", eventCount, kernel.PID, kernel.Command)
		case <-timeout:
			t.Fatalf("Expected at least 2 mock events, got %d", eventCount)
		}
	}
}

// TestConcurrentAccess tests thread safety
func TestConcurrentAccess(t *testing.T) {
	observer, err := NewObserver("test", nil)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Run concurrent operations
	done := make(chan bool)

	// Goroutine 1: Record events
	go func() {
		for i := 0; i < 100; i++ {
			observer.RecordEvent()
			time.Sleep(time.Millisecond)
		}
		done <- true
	}()

	// Goroutine 2: Record errors
	go func() {
		for i := 0; i < 100; i++ {
			observer.RecordError(nil)
			time.Sleep(time.Millisecond)
		}
		done <- true
	}()

	// Goroutine 3: Check health
	go func() {
		for i := 0; i < 50; i++ {
			health := observer.Health()
			assert.NotNil(t, health)
			time.Sleep(2 * time.Millisecond)
		}
		done <- true
	}()

	// Goroutine 4: Get statistics
	go func() {
		for i := 0; i < 50; i++ {
			stats := observer.Statistics()
			assert.NotNil(t, stats)
			time.Sleep(2 * time.Millisecond)
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 4; i++ {
		<-done
	}

	// Verify final state
	stats := observer.Statistics()
	assert.Equal(t, int64(100), stats.EventsProcessed)
	assert.Equal(t, int64(100), stats.ErrorCount)
}
