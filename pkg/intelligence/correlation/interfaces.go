package correlation

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/domain"
	"github.com/yairfalse/tapio/pkg/intelligence/aggregator"
)

// StandardCorrelator is the standard interface all correlators must implement
// Named differently to avoid conflict with existing Correlator interface
type StandardCorrelator interface {
	// Name returns the unique name of this correlator
	Name() string

	// Version returns the version of this correlator
	Version() string

	// Correlate processes an event and returns correlation findings
	Correlate(ctx context.Context, event *domain.UnifiedEvent) (*aggregator.CorrelatorOutput, error)

	// GetCapabilities returns what types of events this correlator can handle
	GetCapabilities() CorrelatorCapabilities

	// Health checks if the correlator is healthy and ready
	Health(ctx context.Context) error
}

// CorrelatorCapabilities describes what a correlator can handle
type CorrelatorCapabilities struct {
	// EventTypes this correlator processes
	EventTypes []string

	// RequiredData fields that must be present in events
	RequiredData []string

	// OptionalData fields that enhance correlation if present
	OptionalData []string

	// Dependencies on external services
	Dependencies []Dependency

	// MaxEventAge is the oldest event this correlator will process
	MaxEventAge time.Duration

	// BatchSupport indicates if this correlator can process multiple events
	BatchSupport bool
}

// Dependency represents an external service dependency
type Dependency struct {
	Name        string
	Type        string // "database", "api", "service"
	Required    bool
	HealthCheck func(ctx context.Context) error
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

// ValidateEvent checks if an event can be processed by this correlator
func (b *BaseCorrelator) ValidateEvent(event *domain.UnifiedEvent) error {
	// Check event type
	typeSupported := false
	eventType := string(event.Type)
	for _, et := range b.capabilities.EventTypes {
		if eventType == et {
			typeSupported = true
			break
		}
	}
	if !typeSupported {
		return &CorrelatorError{
			Type:    ErrorTypeUnsupportedEvent,
			Message: "event type not supported by this correlator",
		}
	}

	// Check event age
	if b.capabilities.MaxEventAge > 0 {
		age := time.Since(event.Timestamp)
		if age > b.capabilities.MaxEventAge {
			return &CorrelatorError{
				Type:    ErrorTypeEventTooOld,
				Message: "event is too old for processing",
			}
		}
	}

	// Check required data
	for _, field := range b.capabilities.RequiredData {
		if !b.hasField(event, field) {
			return &CorrelatorError{
				Type:    ErrorTypeMissingData,
				Message: "required field missing: " + field,
			}
		}
	}

	return nil
}

// hasField checks if an event has a required field
func (b *BaseCorrelator) hasField(event *domain.UnifiedEvent, field string) bool {
	// Check common fields
	switch field {
	case "cluster":
		// Check in K8s context
		if event.K8sContext != nil {
			return true // Cluster might be in labels or annotations
		}
		return false
	case "namespace":
		if event.K8sContext != nil {
			return event.K8sContext.Namespace != ""
		}
		if event.Entity != nil {
			return event.Entity.Namespace != ""
		}
		return false
	case "pod":
		if event.K8sContext != nil && event.K8sContext.Kind == "Pod" {
			return event.K8sContext.Name != ""
		}
		if event.Entity != nil && event.Entity.Type == "pod" {
			return event.Entity.Name != ""
		}
		return false
	case "container":
		// Check in attributes or Kubernetes data
		if event.Attributes != nil {
			if _, ok := event.Attributes["container"]; ok {
				return true
			}
		}
		if event.Kubernetes != nil {
			return true // Assume container info is in K8s data
		}
		return false
	case "node":
		// Check in K8s context or attributes
		if event.K8sContext != nil {
			return event.K8sContext.NodeName != ""
		}
		if event.Attributes != nil {
			if _, ok := event.Attributes["node"]; ok {
				return true
			}
		}
		return false
	case "severity":
		return event.Severity != ""
	default:
		// Check in attributes
		if event.Attributes != nil {
			_, exists := event.Attributes[field]
			return exists
		}
		return false
	}
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

// BatchCorrelator is an interface for correlators that support batch processing
type BatchCorrelator interface {
	StandardCorrelator

	// CorrelateBatch processes multiple events at once
	CorrelateBatch(ctx context.Context, events []*domain.UnifiedEvent) ([]*aggregator.CorrelatorOutput, error)
}

// GraphCorrelator is an interface for correlators that use graph data
type GraphCorrelator interface {
	StandardCorrelator

	// SetGraphClient sets the graph database client
	SetGraphClient(client interface{})

	// PreloadGraph preloads graph data for better performance
	PreloadGraph(ctx context.Context) error
}

// CorrelatorManager manages multiple correlators
type CorrelatorManager struct {
	correlators map[string]StandardCorrelator
}

// NewCorrelatorManager creates a new correlator manager
func NewCorrelatorManager() *CorrelatorManager {
	return &CorrelatorManager{
		correlators: make(map[string]StandardCorrelator),
	}
}

// Register adds a correlator to the manager
func (m *CorrelatorManager) Register(correlator StandardCorrelator) error {
	name := correlator.Name()
	if _, exists := m.correlators[name]; exists {
		return &CorrelatorError{
			Type:    ErrorTypeInternal,
			Message: "correlator already registered: " + name,
		}
	}
	m.correlators[name] = correlator
	return nil
}

// Get returns a correlator by name
func (m *CorrelatorManager) Get(name string) (StandardCorrelator, bool) {
	correlator, exists := m.correlators[name]
	return correlator, exists
}

// GetAll returns all registered correlators
func (m *CorrelatorManager) GetAll() []StandardCorrelator {
	result := make([]StandardCorrelator, 0, len(m.correlators))
	for _, correlator := range m.correlators {
		result = append(result, correlator)
	}
	return result
}

// GetForEvent returns correlators that can handle a specific event
func (m *CorrelatorManager) GetForEvent(event *domain.UnifiedEvent) []StandardCorrelator {
	var result []StandardCorrelator
	for _, correlator := range m.correlators {
		capabilities := correlator.GetCapabilities()
		for _, eventType := range capabilities.EventTypes {
			if eventType == string(event.Type) || eventType == "*" {
				result = append(result, correlator)
				break
			}
		}
	}
	return result
}

// HealthCheck checks health of all correlators
func (m *CorrelatorManager) HealthCheck(ctx context.Context) map[string]error {
	results := make(map[string]error)
	for name, correlator := range m.correlators {
		results[name] = correlator.Health(ctx)
	}
	return results
}
