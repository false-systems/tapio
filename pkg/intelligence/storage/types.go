package storage

import (
	"context"
	"time"

	"github.com/yairfalse/tapio/pkg/intelligence/correlation"
)

// Storage persists and retrieves correlations
type Storage interface {
	// Store a correlation result
	Store(ctx context.Context, result *correlation.CorrelationResult) error

	// Get recent correlations
	GetRecent(ctx context.Context, limit int) ([]*correlation.CorrelationResult, error)

	// Get correlations by trace ID
	GetByTraceID(ctx context.Context, traceID string) ([]*correlation.CorrelationResult, error)

	// Clean up old correlations
	Cleanup(ctx context.Context, olderThan time.Duration) error
}
