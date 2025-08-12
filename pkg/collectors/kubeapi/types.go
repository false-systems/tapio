package kubeapi

import (
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

// ResourceEvent represents a K8s resource change with full context
type ResourceEvent struct {
	// Event metadata
	EventType string // ADDED, MODIFIED, DELETED
	Timestamp time.Time

	// Object identity
	APIVersion string
	Kind       string
	Name       string
	Namespace  string
	UID        types.UID

	// Relationship data (the magic!)
	OwnerReferences []OwnerRef
	Labels          map[string]string
	Annotations     map[string]string

	// State data
	Object          runtime.Object `json:"-"` // Don't serialize, too large
	OldObject       runtime.Object `json:"-"` // For updates
	ResourceVersion string

	// Causality
	Reason  string
	Message string
	Source  EventSource

	// Graph connections
	RelatedObjects []ObjectReference
}

// OwnerRef simplified owner reference
type OwnerRef struct {
	APIVersion string
	Kind       string
	Name       string
	UID        types.UID
}

// ObjectReference points to another object
type ObjectReference struct {
	APIVersion string
	Kind       string
	Name       string
	Namespace  string
	UID        types.UID
	Relation   string // "owns", "managed-by", "mounted-by", etc
}

// EventSource describes what triggered this event
type EventSource struct {
	Component string
	Host      string
}

// TraceContext manages trace propagation
type TraceContext struct {
	traces map[string]string // objectKey -> traceID
}

// ObjectKey creates a unique key for an object
func ObjectKey(kind, namespace, name string) string {
	if namespace == "" {
		return kind + "/" + name
	}
	return kind + "/" + namespace + "/" + name
}
