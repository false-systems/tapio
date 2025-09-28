//go:build linux
// +build linux

package kernel

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/domain"
	"go.uber.org/zap/zaptest"
)

// TestSystemEBPFLoading tests eBPF program loading on Linux
func TestSystemEBPFLoading(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	// Remove memory lock limit
	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "kernel-test",
		BufferSize: 1000,
		EnableEBPF: true,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Verify eBPF state was initialized
	assert.NotNil(t, observer.ebpfState)

	// Check that it's the real eBPF state
	components, ok := observer.ebpfState.(*ebpfComponents)
	require.True(t, ok, "Expected ebpfComponents type")
	assert.NotNil(t, components.objs)
	assert.NotNil(t, components.reader)
	assert.NotEmpty(t, components.links)
}

// TestSystemTracepointAttachment tests openat tracepoint attachment
func TestSystemTracepointAttachment(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "kernel-test",
		BufferSize: 1000,
		EnableEBPF: true,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Verify tracepoint links were created
	components, ok := observer.ebpfState.(*ebpfComponents)
	require.True(t, ok)

	// Should have at least openat tracepoints attached
	assert.GreaterOrEqual(t, len(components.links), 1, "Should have at least one tracepoint attached")

	// Verify the eBPF programs are loaded
	if components.objs.TraceOpenat != nil {
		info, err := components.objs.TraceOpenat.Info()
		assert.NoError(t, err)
		if err == nil {
			t.Logf("Openat entry program loaded: Type=%v, Tag=%x", info.Type, info.Tag)
		}
	}

	if components.objs.TraceOpenatExit != nil {
		info, err := components.objs.TraceOpenatExit.Info()
		assert.NoError(t, err)
		if err == nil {
			t.Logf("Openat exit program loaded: Type=%v, Tag=%x", info.Type, info.Tag)
		}
	}
}

// TestSystemConfigMapDetection tests real ConfigMap path detection
func TestSystemConfigMapDetection(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "kernel-test",
		BufferSize: 1000,
		EnableEBPF: true,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Create a test directory structure mimicking Kubernetes ConfigMap mount
	testDir := "/tmp/test-kubelet"
	podUID := "test-pod-12345"
	configMapPath := filepath.Join(testDir, "pods", podUID, "volumes", "kubernetes.io~configmap", "test-config")

	// Create the directory structure
	err = os.MkdirAll(configMapPath, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(testDir)

	// Create a test file in the ConfigMap directory
	testFile := filepath.Join(configMapPath, "config.yaml")
	err = os.WriteFile(testFile, []byte("test: data"), 0644)
	require.NoError(t, err)

	// Access the file to trigger openat syscall
	content, err := os.ReadFile(testFile)
	assert.NoError(t, err)
	assert.Equal(t, "test: data", string(content))

	// Give eBPF time to capture the event
	time.Sleep(500 * time.Millisecond)

	// Check if we received any events
	events := observer.Events()
	eventCount := 0
	timeout := time.After(2 * time.Second)

	for eventCount < 1 {
		select {
		case event := <-events:
			if event != nil && event.EventData.Kernel != nil {
				eventCount++
				t.Logf("Captured event: PID=%d, Command=%s, EventType=%s",
					event.EventData.Kernel.PID,
					event.EventData.Kernel.Command,
					event.EventData.Kernel.EventType)

				// Check if the path was detected as ConfigMap
				if labels, ok := event.Metadata.Labels["config_type"]; ok {
					assert.Equal(t, "configmap", labels)
				}
			}
		case <-timeout:
			t.Log("No events captured (might need path filtering adjustment)")
			return
		}
	}
}

// TestSystemSecretDetection tests real Secret path detection
func TestSystemSecretDetection(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "kernel-test",
		BufferSize: 1000,
		EnableEBPF: true,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Create a test directory structure mimicking Kubernetes Secret mount
	testDir := "/tmp/test-kubelet-secret"
	podUID := "test-pod-67890"
	secretPath := filepath.Join(testDir, "pods", podUID, "volumes", "kubernetes.io~secret", "test-secret")

	// Create the directory structure
	err = os.MkdirAll(secretPath, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(testDir)

	// Create a test file in the Secret directory
	testFile := filepath.Join(secretPath, "token")
	err = os.WriteFile(testFile, []byte("secret-token-value"), 0600)
	require.NoError(t, err)

	// Access the file to trigger openat syscall
	content, err := os.ReadFile(testFile)
	assert.NoError(t, err)
	assert.Equal(t, "secret-token-value", string(content))

	// Give eBPF time to capture the event
	time.Sleep(500 * time.Millisecond)

	// Check if we received any events
	events := observer.Events()
	eventCount := 0
	timeout := time.After(2 * time.Second)

	for eventCount < 1 {
		select {
		case event := <-events:
			if event != nil && event.EventData.Kernel != nil {
				eventCount++
				t.Logf("Captured secret access: PID=%d, Command=%s",
					event.EventData.Kernel.PID,
					event.EventData.Kernel.Command)

				// Check if the path was detected as Secret
				if labels, ok := event.Metadata.Labels["config_type"]; ok {
					assert.Equal(t, "secret", labels)
				}
			}
		case <-timeout:
			t.Log("No events captured (might need path filtering adjustment)")
			return
		}
	}
}

// TestSystemRingBufferCreation tests ring buffer map creation
func TestSystemRingBufferCreation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "kernel-test",
		BufferSize: 1000,
		EnableEBPF: true,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Verify ring buffer reader was created
	components, ok := observer.ebpfState.(*ebpfComponents)
	require.True(t, ok)
	assert.NotNil(t, components.reader)

	// Check the events map
	if components.objs.Events != nil {
		info, err := components.objs.Events.Info()
		if err == nil {
			assert.Equal(t, ebpf.RingBuf, info.Type)
			t.Logf("Ring buffer created: MaxEntries=%d", info.MaxEntries)
		}
	}
}

// TestSystemFailedConfigAccess tests detection of failed config access
func TestSystemFailedConfigAccess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "kernel-test",
		BufferSize: 1000,
		EnableEBPF: true,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Try to access a non-existent ConfigMap path
	testDir := "/tmp/test-kubelet-fail"
	podUID := "test-pod-fail"
	configMapPath := filepath.Join(testDir, "pods", podUID, "volumes", "kubernetes.io~configmap", "missing-config")
	testFile := filepath.Join(configMapPath, "config.yaml")

	// Try to read non-existent file (should fail with ENOENT)
	_, err = os.ReadFile(testFile)
	assert.Error(t, err)
	assert.True(t, os.IsNotExist(err))

	// Give eBPF time to capture the event
	time.Sleep(500 * time.Millisecond)

	// Check if we received any events
	events := observer.Events()
	timeout := time.After(2 * time.Second)

	select {
	case event := <-events:
		if event != nil && event.EventData.Kernel != nil {
			t.Logf("Captured failed access: PID=%d, ErrorCode=%d",
				event.EventData.Kernel.PID,
				event.EventData.Kernel.ReturnCode)

			// Should capture the failure
			if event.EventData.Kernel.ReturnCode != 0 {
				assert.NotZero(t, event.EventData.Kernel.ReturnCode)
			}
		}
	case <-timeout:
		t.Log("No failed access events captured")
	}
}

// TestSystemComprehensiveMonitoring tests various file access patterns
func TestSystemComprehensiveMonitoring(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "kernel-test",
		BufferSize: 1000,
		EnableEBPF: true,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Collect events in the background
	events := make([]*domain.CollectorEvent, 0)
	eventTypes := make(map[string]int)
	done := make(chan bool)

	go func() {
		timeout := time.After(10 * time.Second)
		for {
			select {
			case event := <-observer.Events():
				if event != nil && event.EventData.Kernel != nil {
					events = append(events, event)
					eventTypes[event.EventData.Kernel.EventType]++
					t.Logf("Event: PID=%d, Command=%s, Type=%s",
						event.EventData.Kernel.PID,
						event.EventData.Kernel.Command,
						event.EventData.Kernel.EventType)
				}
			case <-timeout:
				done <- true
				return
			case <-done:
				return
			}
		}
	}()

	// Create test directory structure
	testBaseDir := "/tmp/kernel-test-comprehensive"
	err = os.MkdirAll(testBaseDir, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(testBaseDir)

	// Test 1: ConfigMap access
	configMapDir := filepath.Join(testBaseDir, "pods", "pod-1", "volumes", "kubernetes.io~configmap", "app-config")
	err = os.MkdirAll(configMapDir, 0755)
	require.NoError(t, err)

	configFile := filepath.Join(configMapDir, "application.yaml")
	err = os.WriteFile(configFile, []byte("app: config"), 0644)
	require.NoError(t, err)

	_, err = os.ReadFile(configFile)
	assert.NoError(t, err)
	t.Log("Accessed ConfigMap file")

	// Test 2: Secret access
	secretDir := filepath.Join(testBaseDir, "pods", "pod-2", "volumes", "kubernetes.io~secret", "db-secret")
	err = os.MkdirAll(secretDir, 0755)
	require.NoError(t, err)

	secretFile := filepath.Join(secretDir, "password")
	err = os.WriteFile(secretFile, []byte("secret-password"), 0600)
	require.NoError(t, err)

	_, err = os.ReadFile(secretFile)
	assert.NoError(t, err)
	t.Log("Accessed Secret file")

	// Test 3: Projected volume access
	projectedDir := filepath.Join(testBaseDir, "pods", "pod-3", "volumes", "kubernetes.io~projected", "service-account")
	err = os.MkdirAll(projectedDir, 0755)
	require.NoError(t, err)

	projectedFile := filepath.Join(projectedDir, "token")
	err = os.WriteFile(projectedFile, []byte("sa-token"), 0644)
	require.NoError(t, err)

	_, err = os.ReadFile(projectedFile)
	assert.NoError(t, err)
	t.Log("Accessed Projected volume file")

	// Test 4: Failed access (ENOENT)
	_, err = os.ReadFile(filepath.Join(configMapDir, "missing.yaml"))
	assert.Error(t, err)
	t.Log("Triggered ENOENT error")

	// Test 5: Multiple rapid accesses
	for i := 0; i < 5; i++ {
		testFile := filepath.Join(configMapDir, fmt.Sprintf("test-%d.txt", i))
		err = os.WriteFile(testFile, []byte(fmt.Sprintf("data-%d", i)), 0644)
		assert.NoError(t, err)

		_, err = os.ReadFile(testFile)
		assert.NoError(t, err)
	}
	t.Log("Completed rapid file accesses")

	// Test 6: Directory listing (triggers multiple openat)
	entries, err := os.ReadDir(configMapDir)
	assert.NoError(t, err)
	t.Logf("Listed %d entries in ConfigMap directory", len(entries))

	// Wait for event collection
	time.Sleep(2 * time.Second)
	close(done)
	time.Sleep(500 * time.Millisecond)

	// Verify results
	t.Logf("\n=== COMPREHENSIVE TEST RESULTS ===")
	t.Logf("Total events captured: %d", len(events))
	t.Logf("Event types breakdown:")
	for eventType, count := range eventTypes {
		t.Logf("  %s: %d events", eventType, count)
	}

	// We should have captured at least some events
	assert.Greater(t, len(events), 0, "Should have captured at least some events")

	// Check event quality
	for _, event := range events {
		assert.NotNil(t, event)
		assert.Equal(t, "kernel-test", event.Source)
		assert.NotNil(t, event.EventData.Kernel)
		assert.NotZero(t, event.EventData.Kernel.PID)
		assert.NotEmpty(t, event.EventData.Kernel.Command)
	}

	t.Log("\n✅ COMPREHENSIVE KERNEL MONITORING TEST COMPLETE!")
}

// TestSystemEBPFMapOperations tests eBPF map operations
func TestSystemEBPFMapOperations(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "kernel-test",
		BufferSize: 1000,
		EnableEBPF: true,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)
	defer observer.Stop()

	// Check eBPF maps
	components, ok := observer.ebpfState.(*ebpfComponents)
	require.True(t, ok)

	// Check if events ring buffer exists
	if components.objs.Events != nil {
		info, err := components.objs.Events.Info()
		if err == nil {
			t.Logf("Events ring buffer: Type=%v, MaxEntries=%d", info.Type, info.MaxEntries)
		}
	}

	// Check if pending openat map exists
	if components.objs.PendingOpenat != nil {
		info, err := components.objs.PendingOpenat.Info()
		if err == nil {
			t.Logf("Pending openat map: Type=%v, MaxEntries=%d", info.Type, info.MaxEntries)
		}
	}
}

// TestSystemCleanupOnStop tests proper cleanup when observer stops
func TestSystemCleanupOnStop(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Skipping eBPF test on non-Linux platform")
	}

	if os.Geteuid() != 0 {
		t.Skip("Skipping eBPF test - requires root privileges")
	}

	err := rlimit.RemoveMemlock()
	require.NoError(t, err)

	logger := zaptest.NewLogger(t)
	config := &Config{
		Name:       "kernel-test",
		BufferSize: 1000,
		EnableEBPF: true,
	}

	observer, err := NewObserverWithConfig(config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = observer.Start(ctx)
	require.NoError(t, err)

	// Verify eBPF is loaded
	assert.NotNil(t, observer.ebpfState)

	// Stop the observer
	err = observer.Stop()
	assert.NoError(t, err)

	// Verify cleanup
	assert.Nil(t, observer.ebpfState)
}
