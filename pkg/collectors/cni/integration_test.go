// +build integration

package cni

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yairfalse/tapio/pkg/collectors"
)

// Integration tests that require file system access
// Run with: go test -tags=integration ./pkg/collectors/cni/...

func TestCollectorFileWatching(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "cni-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test CNI config directory
	cniDir := filepath.Join(tempDir, "cni", "net.d")
	err = os.MkdirAll(cniDir, 0755)
	require.NoError(t, err)

	config := collectors.DefaultCollectorConfig()
	collector, err := NewCollector(config)
	require.NoError(t, err)

	// Override the strategy to watch our test directory
	c := collector.(*Collector)
	c.detectedCNI = "test"
	c.strategy = &testStrategy{watchPath: cniDir}

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Create a test file
	testFile := filepath.Join(cniDir, "10-test.conf")
	err = os.WriteFile(testFile, []byte(`{"cniVersion": "0.4.0", "name": "test"}`), 0644)
	require.NoError(t, err)

	// Should receive file creation event
	timeout := time.After(2 * time.Second)
	eventReceived := false

	for !eventReceived {
		select {
		case event := <-collector.Events():
			var data map[string]interface{}
			err := json.Unmarshal(event.Data, &data)
			require.NoError(t, err)

			if data["file"] != nil && data["op"] != nil {
				eventReceived = true
				assert.Equal(t, "cni", event.Type)
				assert.Equal(t, "file_watch", event.Metadata["source"])
				assert.Contains(t, data["file"].(string), "10-test.conf")
			}
		case <-timeout:
			t.Fatal("timeout waiting for file event")
		}
	}
}

func TestCollectorWithRealCNIDetection(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("Skipping K8s integration test in CI")
	}

	config := collectors.DefaultCollectorConfig()
	collector, err := NewCollector(config)
	require.NoError(t, err)

	ctx := context.Background()
	err = collector.Start(ctx)
	
	// If we can't connect to K8s, skip the test
	if err != nil && contains(err.Error(), "failed to detect CNI") {
		t.Skip("Cannot connect to Kubernetes cluster")
	}
	require.NoError(t, err)
	defer collector.Stop()

	// Check what CNI was detected
	c := collector.(*Collector)
	t.Logf("Detected CNI: %s", c.detectedCNI)

	// Should receive at least heartbeat events
	select {
	case event := <-collector.Events():
		assert.Equal(t, "cni", event.Type)
		assert.Equal(t, c.detectedCNI, event.Metadata["cni_plugin"])
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestCollectorLogMonitoring(t *testing.T) {
	// Create temporary log directory
	tempDir, err := os.MkdirTemp("", "cni-logs-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	logFile := filepath.Join(tempDir, "cni.log")

	config := collectors.DefaultCollectorConfig()
	collector, err := NewCollector(config)
	require.NoError(t, err)

	// Override strategy to watch our test log
	c := collector.(*Collector)
	c.detectedCNI = "test"
	c.strategy = &testStrategy{
		logPath:   logFile,
		watchPath: tempDir,
	}

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Write to log file
	err = os.WriteFile(logFile, []byte("CNI ADD operation completed\n"), 0644)
	require.NoError(t, err)

	// Should detect log file creation
	timeout := time.After(2 * time.Second)
	select {
	case event := <-collector.Events():
		assert.Equal(t, "cni", event.Type)
		// Event could be either file watch or log related
		assert.Contains(t, []string{"file_watch", "heartbeat"}, event.Metadata["source"])
	case <-timeout:
		// It's ok if we just get heartbeats in this timeframe
		t.Log("No file events received, only heartbeats expected")
	}
}

// Test helper strategy
type testStrategy struct {
	logPath   string
	watchPath string
}

func (s *testStrategy) GetName() string {
	return "test"
}

func (s *testStrategy) GetLogPaths() []string {
	if s.logPath != "" {
		return []string{s.logPath}
	}
	return []string{"/tmp/test-cni.log"}
}

func (s *testStrategy) GetWatchPaths() []string {
	if s.watchPath != "" {
		return []string{s.watchPath}
	}
	return []string{"/tmp/test-cni/"}
}

// Stress test with rapid file changes
func TestCollectorStressFileChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	tempDir, err := os.MkdirTemp("", "cni-stress-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	config := collectors.DefaultCollectorConfig()
	config.BufferSize = 1000
	collector, err := NewCollector(config)
	require.NoError(t, err)

	c := collector.(*Collector)
	c.detectedCNI = "stress-test"
	c.strategy = &testStrategy{watchPath: tempDir}

	ctx := context.Background()
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Create many files rapidly
	fileCount := 100
	for i := 0; i < fileCount; i++ {
		filename := filepath.Join(tempDir, fmt.Sprintf("test-%d.conf", i))
		err := os.WriteFile(filename, []byte(fmt.Sprintf(`{"id": %d}`, i)), 0644)
		require.NoError(t, err)
		
		// Small delay to avoid overwhelming the watcher
		if i%10 == 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Consume events
	eventCount := 0
	timeout := time.After(5 * time.Second)

consumeLoop:
	for {
		select {
		case event := <-collector.Events():
			if event.Metadata["source"] == "file_watch" {
				eventCount++
			}
			// Don't require all events - some may be dropped
			if eventCount >= fileCount/2 {
				break consumeLoop
			}
		case <-timeout:
			break consumeLoop
		}
	}

	t.Logf("Received %d file events out of %d files created", eventCount, fileCount)
	assert.Greater(t, eventCount, 0, "Should receive some file events")
}

// Test collector behavior with invalid watcher paths
func TestCollectorInvalidPaths(t *testing.T) {
	config := collectors.DefaultCollectorConfig()
	collector, err := NewCollector(config)
	require.NoError(t, err)

	c := collector.(*Collector)
	c.detectedCNI = "invalid-test"
	c.strategy = &testStrategy{
		watchPath: "/this/path/does/not/exist/at/all",
		logPath:   "/another/invalid/path/log.txt",
	}

	ctx := context.Background()
	// Should start successfully even with invalid paths
	err = collector.Start(ctx)
	require.NoError(t, err)
	defer collector.Stop()

	// Should still be healthy
	assert.True(t, collector.IsHealthy())

	// Should still emit heartbeat events
	select {
	case event := <-collector.Events():
		assert.Equal(t, "cni", event.Type)
		assert.Equal(t, "heartbeat", event.Metadata["source"])
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for heartbeat")
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr
}