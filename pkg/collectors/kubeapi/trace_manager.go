package kubeapi

import (
	"sync"
	"time"

	"github.com/yairfalse/tapio/pkg/collectors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TraceEntry stores trace information with timestamp
type TraceEntry struct {
	TraceID   string
	LastSeen  time.Time
}

// TraceManager handles trace propagation between related K8s objects
type TraceManager struct {
	mu     sync.RWMutex
	traces map[string]*TraceEntry // objectKey -> trace entry

	// Propagation rules
	propagateOwnerRefs bool
	propagateSelectors bool
	
	// Cleanup settings
	maxTraceAge   time.Duration
	cleanupTicker *time.Ticker
	stopCleanup   chan struct{}
}

// NewTraceManager creates a new trace manager
func NewTraceManager() *TraceManager {
	tm := &TraceManager{
		traces:             make(map[string]*TraceEntry),
		propagateOwnerRefs: true,
		propagateSelectors: true,
		maxTraceAge:        30 * time.Minute, // Default: clean up traces older than 30 minutes
		stopCleanup:        make(chan struct{}),
	}
	
	// Start cleanup goroutine
	tm.startCleanup()
	
	return tm
}

// startCleanup starts the background cleanup goroutine
func (tm *TraceManager) startCleanup() {
	tm.cleanupTicker = time.NewTicker(5 * time.Minute) // Run cleanup every 5 minutes
	
	go func() {
		for {
			select {
			case <-tm.cleanupTicker.C:
				tm.cleanupStaleTraces()
			case <-tm.stopCleanup:
				return
			}
		}
	}()
}

// cleanupStaleTraces removes traces that haven't been seen recently
func (tm *TraceManager) cleanupStaleTraces() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	
	now := time.Now()
	cleaned := 0
	
	for key, entry := range tm.traces {
		if now.Sub(entry.LastSeen) > tm.maxTraceAge {
			delete(tm.traces, key)
			cleaned++
		}
	}
	
	// Log cleanup stats if needed (would need logger passed in)
	// tm.logger.Debug("Cleaned stale traces", zap.Int("removed", cleaned), zap.Int("remaining", len(tm.traces)))
}

// Stop stops the trace manager and cleanup goroutine
func (tm *TraceManager) Stop() {
	if tm.cleanupTicker != nil {
		tm.cleanupTicker.Stop()
	}
	close(tm.stopCleanup)
}

// GetOrCreateTrace returns existing trace or creates new one
func (tm *TraceManager) GetOrCreateTrace(obj runtime.Object) string {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Extract metadata
	meta, err := getObjectMeta(obj)
	if err != nil {
		return collectors.GenerateTraceID()
	}

	key := ObjectKey(obj.GetObjectKind().GroupVersionKind().Kind, meta.GetNamespace(), meta.GetName())

	// Check if we already have a trace
	if entry, exists := tm.traces[key]; exists {
		entry.LastSeen = time.Now() // Update last seen
		return entry.TraceID
	}

	// Check if we should inherit from owner
	if tm.propagateOwnerRefs {
		for _, owner := range meta.GetOwnerReferences() {
			ownerKey := ObjectKey(owner.Kind, meta.GetNamespace(), owner.Name)
			if parentEntry, exists := tm.traces[ownerKey]; exists {
				tm.traces[key] = &TraceEntry{
					TraceID:  parentEntry.TraceID,
					LastSeen: time.Now(),
				}
				return parentEntry.TraceID
			}
		}
	}

	// Check annotations for trace
	if annotations := meta.GetAnnotations(); annotations != nil {
		if traceID, exists := annotations["tapio.io/trace-id"]; exists && traceID != "" {
			tm.traces[key] = &TraceEntry{
				TraceID:  traceID,
				LastSeen: time.Now(),
			}
			return traceID
		}
	}

	// Create new trace
	traceID := collectors.GenerateTraceID()
	tm.traces[key] = &TraceEntry{
		TraceID:  traceID,
		LastSeen: time.Now(),
	}
	return traceID
}

// SetTrace explicitly sets trace for an object
func (tm *TraceManager) SetTrace(kind, namespace, name, traceID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	key := ObjectKey(kind, namespace, name)
	tm.traces[key] = &TraceEntry{
		TraceID:  traceID,
		LastSeen: time.Now(),
	}
}

// PropagateTrace propagates trace from parent to child
func (tm *TraceManager) PropagateTrace(parentKind, parentNamespace, parentName, childKind, childNamespace, childName string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	parentKey := ObjectKey(parentKind, parentNamespace, parentName)
	if parentEntry, exists := tm.traces[parentKey]; exists {
		childKey := ObjectKey(childKind, childNamespace, childName)
		tm.traces[childKey] = &TraceEntry{
			TraceID:  parentEntry.TraceID,
			LastSeen: time.Now(),
		}
		parentEntry.LastSeen = time.Now() // Update parent's last seen too
	}
}

// GetTraceForSelector finds trace for objects matching selector
func (tm *TraceManager) GetTraceForSelector(gvk schema.GroupVersionKind, namespace string, selector map[string]string) string {
	if !tm.propagateSelectors || len(selector) == 0 {
		return ""
	}

	tm.mu.RLock()
	defer tm.mu.RUnlock()

	// Look for service that might own this selector
	// This is simplified - in production would use label selectors properly
	for _, entry := range tm.traces {
		if entry != nil && entry.TraceID != "" {
			// Check if this could be a related service
			// Real implementation would check actual selectors
			return entry.TraceID
		}
	}

	return ""
}

// RemoveTrace removes trace for deleted object
func (tm *TraceManager) RemoveTrace(kind, namespace, name string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	key := ObjectKey(kind, namespace, name)
	delete(tm.traces, key)
}

// GetMetrics returns trace manager metrics
func (tm *TraceManager) GetMetrics() map[string]int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	uniqueTraces := make(map[string]bool)
	for _, entry := range tm.traces {
		if entry != nil {
			uniqueTraces[entry.TraceID] = true
		}
	}

	return map[string]int{
		"total_objects": len(tm.traces),
		"unique_traces": len(uniqueTraces),
	}
}
