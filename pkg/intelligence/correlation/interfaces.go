package correlation

import (
	"context"
	"fmt"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
)

// Correlator is the interface all correlators must implement
// Keep it simple and focused on actual functionality
type Correlator interface {
	// Name returns the unique name of this correlator
	Name() string

	// Process analyzes an event and returns correlations found
	Process(ctx context.Context, event *domain.UnifiedEvent) ([]*CorrelationResult, error)
}

// K8sAwareCorrelator is for correlators that need Kubernetes API access
type K8sAwareCorrelator interface {
	Correlator

	// Start initializes K8s watches and caches
	Start(ctx context.Context) error

	// Stop cleanly shuts down K8s watches
	Stop() error
}

// CorrelatorError represents a correlator-specific error
type CorrelatorError struct {
	Type    CorrelatorErrorType
	Message string
	Cause   error
}

// Error implements the error interface
func (e *CorrelatorError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

// CorrelatorErrorType represents types of correlator errors
type CorrelatorErrorType string

const (
	ErrorTypeUnsupportedEvent CorrelatorErrorType = "unsupported_event"
	ErrorTypeEventTooOld      CorrelatorErrorType = "event_too_old"
	ErrorTypeMissingData      CorrelatorErrorType = "missing_data"
	ErrorTypeDependencyFailed CorrelatorErrorType = "dependency_failed"
	ErrorTypeTimeout          CorrelatorErrorType = "timeout"
	ErrorTypeInternal         CorrelatorErrorType = "internal_error"
)

// CorrelatorCapabilities defines what a correlator can handle
type CorrelatorCapabilities struct {
	EventTypes   []string      // Event types this correlator processes
	MaxEventAge  time.Duration // Max age of events to process
	RequiredData []string      // Required fields in events
	OptionalData []string      // Optional fields that enhance correlation
	Dependencies []Dependency  // External dependencies
	BatchSupport bool          // Whether correlator supports batch processing
}

// BaseCorrelator provides common functionality for all correlators
type BaseCorrelator struct {
	name         string
	version      string
	capabilities CorrelatorCapabilities
}

// NewBaseCorrelator creates a new base correlator
func NewBaseCorrelator(name, version string, capabilities CorrelatorCapabilities) *BaseCorrelator {
	return &BaseCorrelator{
		name:         name,
		version:      version,
		capabilities: capabilities,
	}
}

// Name returns the correlator name
func (b *BaseCorrelator) Name() string {
	return b.name
}

// Version returns the correlator version
func (b *BaseCorrelator) Version() string {
	return b.version
}

// GetCapabilities returns the correlator capabilities
func (b *BaseCorrelator) GetCapabilities() CorrelatorCapabilities {
	return b.capabilities
}

// ValidateEvent checks if an event can be processed
func (b *BaseCorrelator) ValidateEvent(event *domain.UnifiedEvent) error {
	// Check event type
	if len(b.capabilities.EventTypes) > 0 {
		supported := false
		for _, et := range b.capabilities.EventTypes {
			if string(event.Type) == et {
				supported = true
				break
			}
		}
		if !supported {
			return &CorrelatorError{
				Type:    ErrorTypeUnsupportedEvent,
				Message: fmt.Sprintf("event type %s not supported", event.Type),
			}
		}
	}

	// Check event age
	if b.capabilities.MaxEventAge > 0 {
		age := time.Since(event.Timestamp)
		if age > b.capabilities.MaxEventAge {
			return &CorrelatorError{
				Type:    ErrorTypeEventTooOld,
				Message: fmt.Sprintf("event is %v old, max age is %v", age, b.capabilities.MaxEventAge),
			}
		}
	}

	// Check required data
	for _, field := range b.capabilities.RequiredData {
		if !b.hasField(event, field) {
			return &CorrelatorError{
				Type:    ErrorTypeMissingData,
				Message: fmt.Sprintf("required field %s is missing", field),
			}
		}
	}

	return nil
}

// hasField checks if an event has a required field
func (b *BaseCorrelator) hasField(event *domain.UnifiedEvent, field string) bool {
	switch field {
	case "cluster":
		if event.K8sContext != nil && event.K8sContext.ClusterName != "" {
			return true
		}
		return event.Attributes != nil && event.Attributes["cluster"] != nil && event.Attributes["cluster"] != ""
	case "namespace":
		return event.K8sContext != nil && event.K8sContext.Namespace != ""
	case "pod":
		return event.K8sContext != nil && event.K8sContext.Name != ""
	case "container":
		if event.Attributes != nil {
			if val, ok := event.Attributes["container"]; ok && val != "" {
				return true
			}
		}
		return false
	case "node":
		return event.K8sContext != nil && event.K8sContext.NodeName != ""
	case "severity":
		return event.Severity != ""
	default:
		// Check in attributes
		if event.Attributes != nil {
			if val, ok := event.Attributes[field]; ok && val != nil {
				return true
			}
		}
		return false
	}
}
