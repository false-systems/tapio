package correlation

import (
	"context"
	"time"
)

// Storage defines the interface for persisting and retrieving correlations
// This interface is implemented by storage adapters in pkg/integrations/storage/
type Storage interface {
	// Store saves a correlation result
	Store(ctx context.Context, result *CorrelationResult) error

	// GetRecent retrieves recent correlations
	GetRecent(ctx context.Context, limit int) ([]*CorrelationResult, error)

	// GetByTraceID retrieves correlations for a specific trace
	GetByTraceID(ctx context.Context, traceID string) ([]*CorrelationResult, error)

	// GetByTimeRange retrieves correlations within a time range
	GetByTimeRange(ctx context.Context, start, end time.Time) ([]*CorrelationResult, error)

	// GetByResource retrieves correlations affecting a specific resource
	GetByResource(ctx context.Context, resourceType, namespace, name string) ([]*CorrelationResult, error)

	// Cleanup removes old correlations
	Cleanup(ctx context.Context, olderThan time.Duration) error
}
