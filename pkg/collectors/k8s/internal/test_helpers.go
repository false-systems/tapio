package internal

import (
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/k8s/core"
)

// createTestConfig creates a test configuration
func createTestConfig() core.Config {
	return core.Config{
		Name:            "test",
		Enabled:         true,
		EventBufferSize: 100,
		Namespace:       "default",
		ResyncPeriod:    30 * time.Minute,
	}
}

// testWatcherNoInformer tests watcher without k8s dependencies
func testWatcherNoInformer(t *testing.T, watcher *baseWatcher) {
	if watcher.resourceType == "" {
		t.Error("Expected resource type to be set")
	}

	if watcher.eventChan == nil {
		t.Error("Expected event channel to be created")
	}

	if cap(watcher.eventChan) != 100 {
		t.Errorf("Expected channel capacity 100, got %d", cap(watcher.eventChan))
	}
}