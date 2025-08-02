package kubeapi

import (
	"sync"

	"github.com/yairfalse/tapio/pkg/collectors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// TraceManager handles trace propagation between related K8s objects
type TraceManager struct {
	mu     sync.RWMutex
	traces map[string]string // objectKey -> traceID

	// Propagation rules
	propagateOwnerRefs bool
	propagateSelectors bool
}

// NewTraceManager creates a new trace manager
func NewTraceManager() *TraceManager {
	return &TraceManager{
		traces:             make(map[string]string),
		propagateOwnerRefs: true,
		propagateSelectors: true,
	}
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
	if trace, exists := tm.traces[key]; exists {
		return trace
	}

	// Check if we should inherit from owner
	if tm.propagateOwnerRefs {
		for _, owner := range meta.GetOwnerReferences() {
			ownerKey := ObjectKey(owner.Kind, meta.GetNamespace(), owner.Name)
			if parentTrace, exists := tm.traces[ownerKey]; exists {
				tm.traces[key] = parentTrace
				return parentTrace
			}
		}
	}

	// Check annotations for trace
	if annotations := meta.GetAnnotations(); annotations != nil {
		if trace, exists := annotations["tapio.io/trace-id"]; exists && trace != "" {
			tm.traces[key] = trace
			return trace
		}
	}

	// Create new trace
	trace := collectors.GenerateTraceID()
	tm.traces[key] = trace
	return trace
}

// SetTrace explicitly sets trace for an object
func (tm *TraceManager) SetTrace(kind, namespace, name, traceID string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	key := ObjectKey(kind, namespace, name)
	tm.traces[key] = traceID
}

// PropagateTrace propagates trace from parent to child
func (tm *TraceManager) PropagateTrace(parentKind, parentNamespace, parentName, childKind, childNamespace, childName string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	parentKey := ObjectKey(parentKind, parentNamespace, parentName)
	if parentTrace, exists := tm.traces[parentKey]; exists {
		childKey := ObjectKey(childKind, childNamespace, childName)
		tm.traces[childKey] = parentTrace
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
	for _, trace := range tm.traces {
		if trace != "" {
			// Check if this could be a related service
			// Real implementation would check actual selectors
			return trace
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
	for _, trace := range tm.traces {
		uniqueTraces[trace] = true
	}

	return map[string]int{
		"total_objects": len(tm.traces),
		"unique_traces": len(uniqueTraces),
	}
}
