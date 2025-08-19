package domain

import (
	"time"
)

// ResourceEvent represents a K8s resource change event in the domain layer
// This is a domain abstraction that doesn't depend on collector-specific types
type ResourceEvent struct {
	// Event metadata
	EventType string // ADDED, MODIFIED, DELETED
	Timestamp time.Time

	// Object identity
	APIVersion string
	Kind       string
	Name       string
	Namespace  string
	UID        string

	// Relationship data
	OwnerReferences []ResourceOwnerRef
	Labels          map[string]string
	Annotations     map[string]string

	// State data - using interface{} here is acceptable for k8s objects
	// as they are inherently dynamic and we need to preserve compatibility
	Object          interface{} `json:"-"` // Don't serialize, too large
	OldObject       interface{} `json:"-"` // For updates
	ResourceVersion string

	// Causality
	Reason  string
	Message string
	Source  ResourceEventSource

	// Graph connections
	RelatedObjects []ResourceObjectReference
}

// ResourceOwnerRef simplified owner reference
type ResourceOwnerRef struct {
	APIVersion string
	Kind       string
	Name       string
	UID        string
}

// ResourceObjectReference points to another object
type ResourceObjectReference struct {
	APIVersion string
	Kind       string
	Name       string
	Namespace  string
	UID        string
	Relation   string // "owns", "managed-by", "mounted-by", etc
}

// ResourceEventSource describes what triggered this event
type ResourceEventSource struct {
	Component string
	Host      string
}
