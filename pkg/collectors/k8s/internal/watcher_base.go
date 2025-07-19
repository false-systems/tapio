package internal

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors/k8s/core"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

// baseWatcher provides common functionality for all resource watchers
type baseWatcher struct {
	resourceType string
	namespace    string
	config       core.Config
	eventChan    chan core.RawEvent
	ctx          context.Context
	cancel       context.CancelFunc
	informer     cache.SharedInformer
}

// newBaseWatcher creates a new base watcher
func newBaseWatcher(resourceType string, config core.Config) *baseWatcher {
	return &baseWatcher{
		resourceType: resourceType,
		namespace:    config.Namespace,
		config:       config,
		eventChan:    make(chan core.RawEvent, 100),
	}
}

// Start starts the watcher
func (w *baseWatcher) Start(ctx context.Context) error {
	w.ctx, w.cancel = context.WithCancel(ctx)

	if w.informer == nil {
		return fmt.Errorf("informer not initialized for %s watcher", w.resourceType)
	}

	// Add event handlers
	w.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    w.handleAdd,
		UpdateFunc: w.handleUpdate,
		DeleteFunc: w.handleDelete,
	})

	// Start informer
	go w.informer.Run(w.ctx.Done())

	// Wait for cache sync
	if !cache.WaitForCacheSync(w.ctx.Done(), w.informer.HasSynced) {
		return fmt.Errorf("failed to sync cache for %s", w.resourceType)
	}

	return nil
}

// Stop stops the watcher
func (w *baseWatcher) Stop() error {
	if w.cancel != nil {
		w.cancel()
	}
	close(w.eventChan)
	return nil
}

// Events returns the event channel
func (w *baseWatcher) Events() <-chan core.RawEvent {
	return w.eventChan
}

// ResourceType returns the resource type being watched
func (w *baseWatcher) ResourceType() string {
	return w.resourceType
}

// handleAdd handles resource addition
func (w *baseWatcher) handleAdd(obj interface{}) {
	w.sendEvent(core.EventTypeAdded, obj, nil)
}

// handleUpdate handles resource updates
func (w *baseWatcher) handleUpdate(oldObj, newObj interface{}) {
	w.sendEvent(core.EventTypeModified, newObj, oldObj)
}

// handleDelete handles resource deletion
func (w *baseWatcher) handleDelete(obj interface{}) {
	// Handle DeletedFinalStateUnknown
	if deletedFinal, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		obj = deletedFinal.Obj
	}
	w.sendEvent(core.EventTypeDeleted, obj, nil)
}

// sendEvent sends an event to the channel
func (w *baseWatcher) sendEvent(eventType core.EventType, obj, oldObj interface{}) {
	// Extract metadata
	meta, err := w.extractMetadata(obj)
	if err != nil {
		// Log error and return
		return
	}

	event := core.RawEvent{
		Type:         eventType,
		Object:       obj,
		OldObject:    oldObj,
		ResourceKind: w.resourceType,
		Namespace:    meta.GetNamespace(),
		Name:         meta.GetName(),
		Timestamp:    time.Now(),
	}

	select {
	case w.eventChan <- event:
		// Event sent successfully
	case <-w.ctx.Done():
		// Context cancelled, stop sending
		return
	default:
		// Channel full, drop event
		// In production, you'd want to track this metric
	}
}

// extractMetadata extracts metadata from an object
func (w *baseWatcher) extractMetadata(obj interface{}) (metav1.Object, error) {
	switch o := obj.(type) {
	case metav1.Object:
		return o, nil
	default:
		return nil, fmt.Errorf("object does not have metadata")
	}
}

// createListOptions creates list options for the watcher
func (w *baseWatcher) createListOptions() metav1.ListOptions {
	opts := metav1.ListOptions{}

	if w.config.LabelSelector != "" {
		opts.LabelSelector = w.config.LabelSelector
	}

	if w.config.FieldSelector != "" {
		opts.FieldSelector = w.config.FieldSelector
	}

	return opts
}

// getWatchOptions returns watch options with timeout
func (w *baseWatcher) getWatchOptions() metav1.ListOptions {
	opts := w.createListOptions()
	opts.Watch = true
	opts.AllowWatchBookmarks = true
	return opts
}
