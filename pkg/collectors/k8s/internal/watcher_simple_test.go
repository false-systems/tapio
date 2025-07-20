package internal

import (
	"context"
	"testing"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/k8s/core"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

func TestBaseWatcherBasics(t *testing.T) {
	config := core.Config{
		Namespace:       "default",
		EventBufferSize: 100,
		LabelSelector:   "app=test",
		FieldSelector:   "metadata.name=test-pod",
	}

	watcher := newBaseWatcher("Pod", config)

	// Test basic properties
	if watcher.resourceType != "Pod" {
		t.Errorf("Expected resource type Pod, got %s", watcher.resourceType)
	}

	if watcher.namespace != "default" {
		t.Errorf("Expected namespace default, got %s", watcher.namespace)
	}

	if watcher.eventChan == nil {
		t.Error("Expected event channel to be created")
	}

	// Test ResourceType method
	if watcher.ResourceType() != "Pod" {
		t.Errorf("ResourceType() = %s, want Pod", watcher.ResourceType())
	}

	// Test Events method
	events := watcher.Events()
	if events == nil {
		t.Error("Events() returned nil channel")
	}

	// Test list options
	listOpts := watcher.createListOptions()
	if listOpts.LabelSelector != "app=test" {
		t.Errorf("Expected label selector app=test, got %s", listOpts.LabelSelector)
	}

	if listOpts.FieldSelector != "metadata.name=test-pod" {
		t.Errorf("Expected field selector metadata.name=test-pod, got %s", listOpts.FieldSelector)
	}

	// Test watch options
	watchOpts := watcher.getWatchOptions()
	if !watchOpts.Watch {
		t.Error("Expected watch to be true")
	}

	if !watchOpts.AllowWatchBookmarks {
		t.Error("Expected allow watch bookmarks to be true")
	}
}

func TestBaseWatcherEventSending(t *testing.T) {
	config := core.Config{
		Namespace:       "default",
		EventBufferSize: 10,
	}

	watcher := newBaseWatcher("Pod", config)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcher.ctx = ctx
	watcher.cancel = cancel

	// Create a test pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	// Send event
	go func() {
		watcher.sendEvent(core.EventTypeAdded, pod, nil)
	}()

	// Receive event
	select {
	case event := <-watcher.eventChan:
		if event.Type != core.EventTypeAdded {
			t.Errorf("Expected event type ADDED, got %s", event.Type)
		}
		if event.Name != "test-pod" {
			t.Errorf("Expected name test-pod, got %s", event.Name)
		}
		if event.ResourceKind != "Pod" {
			t.Errorf("Expected resource kind Pod, got %s", event.ResourceKind)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for event")
	}
}

func TestBaseWatcherStop(t *testing.T) {
	config := core.Config{
		Namespace:       "default",
		EventBufferSize: 10,
	}

	watcher := newBaseWatcher("Pod", config)
	ctx, cancel := context.WithCancel(context.Background())
	watcher.ctx = ctx
	watcher.cancel = cancel

	// Stop the watcher
	err := watcher.Stop()
	if err != nil {
		t.Errorf("Failed to stop watcher: %v", err)
	}

	// Try to receive from closed channel
	_, ok := <-watcher.eventChan
	if ok {
		t.Error("Expected event channel to be closed")
	}
}

func TestExtractMetadataFromPod(t *testing.T) {
	config := core.Config{}
	watcher := newBaseWatcher("Pod", config)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			Labels: map[string]string{
				"app": "test",
			},
		},
	}

	meta, err := watcher.extractMetadata(pod)
	if err != nil {
		t.Errorf("Failed to extract metadata: %v", err)
	}

	if meta.GetName() != "test-pod" {
		t.Errorf("Expected name test-pod, got %s", meta.GetName())
	}

	if meta.GetNamespace() != "default" {
		t.Errorf("Expected namespace default, got %s", meta.GetNamespace())
	}

	labels := meta.GetLabels()
	if labels["app"] != "test" {
		t.Errorf("Expected label app=test, got %v", labels["app"])
	}
}

func TestDeletedFinalStateUnknownHandling(t *testing.T) {
	config := core.Config{
		Namespace:       "default",
		EventBufferSize: 10,
	}

	watcher := newBaseWatcher("Pod", config)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcher.ctx = ctx
	watcher.cancel = cancel

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
	}

	// Create DeletedFinalStateUnknown
	deletedFinal := cache.DeletedFinalStateUnknown{
		Key: "default/test-pod",
		Obj: pod,
	}

	// Send as delete event
	go func() {
		watcher.handleDelete(deletedFinal)
	}()

	// Verify event is received correctly
	select {
	case event := <-watcher.eventChan:
		if event.Type != core.EventTypeDeleted {
			t.Errorf("Expected event type DELETED, got %s", event.Type)
		}
		if event.Name != "test-pod" {
			t.Errorf("Expected name test-pod, got %s", event.Name)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for event")
	}
}

func TestSpecificWatchers(t *testing.T) {
	config := createTestConfig()

	// Just test that watchers can be created
	// We can't test their informers without a real k8s client
	tests := []struct {
		name         string
		createFunc   func() core.ResourceWatcher
		resourceType string
	}{
		{
			name: "pod watcher",
			createFunc: func() core.ResourceWatcher {
				return newBaseWatcher("Pod", config)
			},
			resourceType: "Pod",
		},
		{
			name: "node watcher",
			createFunc: func() core.ResourceWatcher {
				return newBaseWatcher("Node", config)
			},
			resourceType: "Node",
		},
		{
			name: "service watcher",
			createFunc: func() core.ResourceWatcher {
				return newBaseWatcher("Service", config)
			},
			resourceType: "Service",
		},
		{
			name: "deployment watcher",
			createFunc: func() core.ResourceWatcher {
				return newBaseWatcher("Deployment", config)
			},
			resourceType: "Deployment",
		},
		{
			name: "event watcher",
			createFunc: func() core.ResourceWatcher {
				return newBaseWatcher("Event", config)
			},
			resourceType: "Event",
		},
		{
			name: "configmap watcher",
			createFunc: func() core.ResourceWatcher {
				return newBaseWatcher("ConfigMap", config)
			},
			resourceType: "ConfigMap",
		},
		{
			name: "secret watcher",
			createFunc: func() core.ResourceWatcher {
				return newBaseWatcher("Secret", config)
			},
			resourceType: "Secret",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			watcher := tt.createFunc()
			if watcher.ResourceType() != tt.resourceType {
				t.Errorf("Expected resource type %s, got %s", tt.resourceType, watcher.ResourceType())
			}
		})
	}
}
