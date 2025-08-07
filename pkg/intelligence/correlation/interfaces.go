package correlation

import (
	"context"

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
